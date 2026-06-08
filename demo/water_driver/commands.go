package water_driver

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

const commandPath = "WaterWorks"

type commandListener struct {
	nc      *natsgo.Conn
	sub     *natsgo.Subscription
	network *Network
	subject string
}

type commandResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func startCommandListener(network *Network) (*commandListener, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = natsgo.DefaultURL
	}

	opts := []natsgo.Option{
		natsgo.Name("water-driver"),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(2 * time.Second),
		natsgo.DisconnectErrHandler(func(_ *natsgo.Conn, err error) {
			log.Printf("water_driver: NATS connection lost: %v", err)
		}),
	}
	if password := os.Getenv("NATS_INTERNAL_PASSWORD"); password != "" {
		opts = append(opts, natsgo.UserInfo("internal", password))
	}

	nc, err := natsgo.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("NATS connect: %w", err)
	}

	subject := fmt.Sprintf("xact.command.%s.%s.>", org, commandPath)
	listener := &commandListener{nc: nc, network: network, subject: subject}
	sub, err := nc.QueueSubscribe(subject, "water-driver", listener.handle)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe %s: %w", subject, err)
	}
	listener.sub = sub

	if err := nc.Flush(); err != nil {
		nc.Close()
		return nil, fmt.Errorf("flush subscription: %w", err)
	}
	log.Printf("water_driver: command listener subscribed to %s", subject)
	return listener, nil
}

func (l *commandListener) close() {
	if l == nil {
		return
	}
	if l.sub != nil {
		_ = l.sub.Unsubscribe()
	}
	if l.nc != nil {
		l.nc.Close()
	}
}

func (l *commandListener) handle(msg *natsgo.Msg) {
	resp := l.applyForSubject(msg.Subject, msg.Data)
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("water_driver: marshal command response: %v", err)
		return
	}
	if err := msg.Respond(data); err != nil {
		log.Printf("water_driver: respond to command: %v", err)
	}
}

func (l *commandListener) apply(data []byte) commandResponse {
	return l.applyForSubject("", data)
}

func (l *commandListener) applyForSubject(subject string, data []byte) commandResponse {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return commandResponse{Success: false, Message: "invalid command payload"}
	}

	id := readCommandID(payload["id"])
	defaultStationID := stationIDFromCommandSubject(subject)
	applied := 0
	var failures []string

	for path, raw := range payload {
		if path == "id" {
			continue
		}

		stationID, index, field, ok := parsePumpCommandPath(path, defaultStationID)
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: unsupported path", path))
			continue
		}

		running, err := parseCommandBool(raw)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}

		var applyErr error
		switch field {
		case "status":
			applyErr = l.network.SetPumpStatus(stationID, index, running)
		case "manual":
			applyErr = l.network.SetPumpManual(stationID, index, running)
		}
		if applyErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, applyErr))
			continue
		}
		applied++
	}

	if applied == 0 {
		msg := "no pump command applied"
		if len(failures) > 0 {
			msg = strings.Join(failures, "; ")
		}
		return commandResponse{ID: id, Success: false, Message: msg}
	}
	if len(failures) > 0 {
		return commandResponse{ID: id, Success: false, Message: strings.Join(failures, "; ")}
	}
	return commandResponse{ID: id, Success: true, Message: "The command succeeded"}
}

func stationIDFromCommandSubject(subject string) string {
	normalized := strings.Trim(strings.ReplaceAll(subject, "/", "."), ".")
	if normalized == "" {
		return ""
	}
	segments := strings.Split(normalized, ".")
	for i := 0; i < len(segments); i++ {
		if segments[i] != commandPath {
			continue
		}
		if i+2 < len(segments) {
			return segments[i+2]
		}
		return ""
	}
	return ""
}

func readCommandID(raw json.RawMessage) string {
	var id string
	_ = json.Unmarshal(raw, &id)
	return id
}

func parsePumpStatusPath(path, defaultStationID string) (string, int, bool) {
	stationID, index, field, ok := parsePumpCommandPath(path, defaultStationID)
	if !ok || field != "status" {
		return "", 0, false
	}
	return stationID, index, true
}

func parsePumpCommandPath(path, defaultStationID string) (string, int, string, bool) {
	normalized := strings.Trim(strings.ReplaceAll(path, "/", "."), ".")
	segments := strings.Split(normalized, ".")
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] != "pumps" {
			continue
		}
		field := segments[i+2]
		if field != "status" && field != "manual" {
			continue
		}
		if i+3 != len(segments) {
			return "", 0, "", false
		}
		index, err := strconv.Atoi(segments[i+1])
		if err != nil || index < 0 || index > 1 {
			return "", 0, "", false
		}
		stationID := defaultStationID
		if stationID == "" {
			stationID = "RAW_WATER_PS"
		}
		if i > 0 {
			stationID = segments[i-1]
		}
		return stationID, index, field, true
	}
	return "", 0, "", false
}

func parseCommandBool(raw json.RawMessage) (bool, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("invalid value")
	}

	switch v := value.(type) {
	case bool:
		return v, nil
	case float64:
		return v != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "on", "run", "running", "start", "started":
			return true, nil
		case "0", "false", "off", "stop", "stopped":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected boolean, 0/1, or on/off")
}
