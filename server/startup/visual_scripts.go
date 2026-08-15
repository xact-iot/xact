package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/events"
	xactnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/visualscripts"
)

func visualScriptServices(database sqldb.DB, treeOps *tree.TreeWithOperations, nc *natsgo.Conn, publisher *events.Publisher) (visualscripts.RuntimeServices, *natsgo.Subscription, error) {
	router := visualscripts.NewTagChangeRouter(1000, 100)
	services := visualscripts.RuntimeServices{
		TagRouter:  router,
		CanExecute: xactnats.MayExecuteVisualScripts,
		SetTag: func(_ context.Context, msg visualscripts.Message, _ string, path string, value any) error {
			path, err := tenantTagPath(msg.OrgName, path)
			if err != nil {
				return err
			}
			return treeOps.SetLeafValue(path, value)
		},
		SendControl: func(ctx context.Context, msg visualscripts.Message, _ string, device, path string, value any, timeout time.Duration) error {
			device = strings.TrimSpace(device)
			if device == "" || strings.ContainsAny(device, ".*/>") {
				return errors.New("device name is invalid")
			}
			path = visualscripts.NormalizePathPattern(path)
			if path == "" {
				return errors.New("control path is required")
			}
			prefix := msg.OrgName + "." + device + "."
			path = strings.TrimPrefix(path, prefix)
			path = strings.TrimPrefix(path, device+".")
			id := msg.ID
			if id == "" {
				id = msg.CorrelationID
			}
			payload, err := json.Marshal(map[string]any{"id": id, path: value})
			if err != nil {
				return err
			}
			requestCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			response, err := nc.RequestWithContext(requestCtx, xactnats.CommandSubjectPrefix+msg.OrgName+"."+device, payload)
			if err != nil {
				return fmt.Errorf("command request failed: %w", err)
			}
			var result struct {
				ID      string `json:"id"`
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(response.Data, &result); err != nil {
				return errors.New("command response was invalid")
			}
			if result.ID != "" && result.ID != id {
				return errors.New("command response ID did not match")
			}
			if !result.Success {
				return fmt.Errorf("command failed: %s", result.Message)
			}
			return nil
		},
		SendNotification: func(ctx context.Context, msg visualscripts.Message, nodeID, profile, severity, message, device string) error {
			profileID, err := database.ResolveNotificationID(ctx, msg.OrgName, strings.TrimSpace(profile))
			if err != nil {
				return fmt.Errorf("notification profile: %w", err)
			}
			return publishVisualScriptEvent(publisher, msg, nodeID, severity, message, device, profileID)
		},
		LogEvent: func(_ context.Context, msg visualscripts.Message, nodeID, severity, message, device string) error {
			return publishVisualScriptEvent(publisher, msg, nodeID, severity, message, device, 0)
		},
	}
	if nc == nil {
		return services, nil, nil
	}
	subscription, err := nc.Subscribe(xactnats.BroadcastStreamPrefix+string(xactnats.TagValueStream)+".>", func(message *natsgo.Msg) {
		dispatchVisualScriptTag(router, message.Subject, message.Data)
	})
	return services, subscription, err
}

func tenantTagPath(org, path string) (string, error) {
	path = visualscripts.NormalizePathPattern(path)
	if path == "" || strings.ContainsAny(path, "*?") {
		return "", errors.New("Set Tag requires an exact tag path")
	}
	if strings.HasPrefix(path, org+".") {
		return path, nil
	}
	first, _, _ := strings.Cut(path, ".")
	if first == "" {
		return "", errors.New("tag path is invalid")
	}
	return org + "." + path, nil
}

func publishVisualScriptEvent(publisher *events.Publisher, msg visualscripts.Message, nodeID, severity, message, device string, notificationID int) error {
	if publisher == nil {
		return errors.New("event publisher is unavailable")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("event message is required")
	}
	entry := events.EventEntry{Timestamp: time.Now().UTC(), OrgName: msg.OrgName, Severity: normalizeEventSeverity(severity), NotificationID: notificationID, Device: strings.TrimSpace(device), Message: message, Params: map[string]any{
		"scriptId": msg.ScriptID, "revision": msg.ActiveRevision, "runId": msg.CorrelationID, "nodeId": nodeID, "triggerNodeId": msg.TriggerNodeID, "msgId": msg.ID,
	}}
	return publisher.PublishEntry(entry)
}

func normalizeEventSeverity(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG", "WARN", "ERROR", "CRITICAL":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return "INFO"
	}
}

func dispatchVisualScriptTag(router *visualscripts.TagChangeRouter, subject string, data []byte) {
	prefix := xactnats.BroadcastStreamPrefix + string(xactnats.TagValueStream) + "."
	remainder := strings.TrimPrefix(subject, prefix)
	parts := strings.Split(remainder, ".")
	if len(parts) < 2 || remainder == subject {
		return
	}
	org := parts[0]
	path := strings.Join(parts[1:], ".")
	device := parts[1]
	var values map[string]struct {
		Value     any    `json:"value"`
		Status    string `json:"status"`
		Timestamp int64  `json:"timestamp"`
	}
	if json.Unmarshal(data, &values) != nil {
		return
	}
	for _, value := range values {
		timestamp := time.Now().UTC()
		if value.Timestamp > 0 {
			timestamp = time.UnixMilli(value.Timestamp).UTC()
		}
		router.Dispatch(visualscripts.TagChange{OrgName: org, DevicePath: device, TagPath: path, Value: value.Value, Timestamp: timestamp, Fields: map[string]any{"status": value.Status, "sourceTimestamp": timestamp.Format(time.RFC3339Nano)}})
	}
}
