package iss_driver

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

type IngestEvent struct {
	Tenant     string  `json:"tenant"`
	Zone       string  `json:"zone"`
	DeviceType string  `json:"device_type"`
	DeviceName string  `json:"device_name"`
	TagData    TagData `json:"tag_data"`
}
type TagData struct {
	Groups      map[string]map[string]any
	DirectTags  map[string]any
	TSUnixMilli int64
}

type ingestResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// natsPublisher publishes ISS telemetry directly to the XACT NATS cluster
// using a standard NATS client - no XACT server internals required.
type natsPublisher struct {
	nc      *natsgo.Conn
	subject string
}

func newNATSPublisher() (*natsPublisher, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = natsgo.DefaultURL
	}
	password := os.Getenv("NATS_INTERNAL_PASSWORD")

	opts := []natsgo.Option{
		natsgo.Name("iss-driver"),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(2 * time.Second),
		natsgo.DisconnectErrHandler(func(_ *natsgo.Conn, err error) {
			fmt.Printf("[ISS] NATS connection lost: %v\n", err)
		}),
	}
	if password != "" {
		opts = append(opts, natsgo.UserInfo("internal", password))
	}

	nc, err := natsgo.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("NATS connect: %w", err)
	}
	// Subject uses the admitted request/reply ingest path:
	// xact.internal.ingest_request.{tenant}[.zone.{zone}].{deviceType}.{deviceName}
	subject := "xact.internal.ingest_request.default.NASA.ISS"
	return &natsPublisher{nc: nc, subject: subject}, nil
}

// publish sends tag data as JSON to the cluster via NATS.
// groupVals maps groupName → tagName → value, matching the ingest payload format.
func (p *natsPublisher) publish(groupVals map[string]map[string]any) error {
	var evt IngestEvent
	evt.Tenant = "default"
	evt.Zone = ""
	evt.DeviceType = "NASA"
	evt.DeviceName = "ISS"
	evt.TagData = TagData{}
	evt.TagData.Groups = groupVals
	evt.TagData.TSUnixMilli = time.Now().UnixMilli()

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	reply, err := p.nc.Request(p.subject, data, 10*time.Second)
	if err != nil {
		return err
	}
	var resp ingestResponse
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return err
	}
	if resp.Status != "accepted" {
		if resp.Error != "" {
			return fmt.Errorf("ingest %s: %s", resp.Status, resp.Error)
		}
		return fmt.Errorf("ingest %s", resp.Status)
	}
	return nil
}

func (p *natsPublisher) disconnect() {
	if p.nc != nil {
		p.nc.Close()
	}
}
