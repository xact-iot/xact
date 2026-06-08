package mqtt

import (
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/rtdb/ingest"
)

// Message represents a parsed MQTT message ready for processing.
type Message struct {
	Topic      string
	Tenant     string
	Zone       string
	MsgType    string
	DeviceType string
	DeviceName string
	TagData    ingest.TagData
	EnqueuedAt time.Time
	Ack        func()
}

// TopicPatternZoned is the MQTT topic pattern for messages with zone.
// xact/{msgtype}/{tenant}/zone/{zone}/{devicetype}/{devicename}
const TopicPatternZoned = "xact/data/+/zone/+/+/+"

// TopicPattern is the MQTT topic pattern for messages without zone.
// xact/{msgtype}/{tenant}/{devicetype}/{devicename}
const TopicPattern = "xact/data/+/+/+"

// ParseTopic extracts components from an MQTT topic.
// Formats:
//   - Zoned: xact/{msgtype}/{tenant}/zone/{zone}/{devicetype}/{devicename} (7 parts)
//   - Zoneless: xact/{msgtype}/{tenant}/{devicetype}/{devicename} (5 parts)
func ParseTopic(topic string) (tenant, zone, msgType, deviceType, deviceName string, err error) {
	parts := strings.Split(topic, "/")
	switch len(parts) {
	case 7:
		if parts[0] != "xact" || parts[3] != "zone" {
			return "", "", "", "", "", fmt.Errorf("invalid zoned topic format: expected xact/{msgtype}/{tenant}/zone/{zone}/{devicetype}/{devicename}")
		}
		return parts[2], parts[4], parts[1], parts[5], parts[6], nil
	case 5:
		if parts[0] != "xact" {
			return "", "", "", "", "", fmt.Errorf("invalid topic prefix: expected 'xact', got '%s'", parts[0])
		}
		return parts[2], "", parts[1], parts[3], parts[4], nil
	default:
		return "", "", "", "", "", fmt.Errorf("invalid topic format: expected 5 or 7 parts, got %d", len(parts))
	}
}

// ParsePayload parses an MQTT message payload.
// It delegates to ingest.ParsePayload which handles both scalar and grouped tags.
func ParsePayload(data []byte) (ingest.TagData, error) {
	return ingest.ParsePayload(data)
}

// DetermineType infers the RTDB scalar type from a Go value.
// Delegates to ingest.DetermineType.
var DetermineType = ingest.DetermineType

// BuildRtdbPath constructs an RTDB path for a grouped tag.
// Format with zone: /{tenant}/zone/{zone}/{devicetype}/{devicename}/{taggroup}/{tagname}
// Format without zone: /{tenant}/{devicetype}/{devicename}/{taggroup}/{tagname}
func BuildRtdbPath(tenant, zone, deviceType, deviceName, tagGroup, tagName string) string {
	if tenant == "" {
		tenant = "default"
	}
	if zone != "" {
		return fmt.Sprintf("/%s/zone/%s/%s/%s/%s/%s", tenant, zone, deviceType, deviceName, tagGroup, tagName)
	}
	return fmt.Sprintf("/%s/%s/%s/%s/%s", tenant, deviceType, deviceName, tagGroup, tagName)
}

// Timestamp returns the message timestamp, falling back to now.
func (m *Message) Timestamp() int64 {
	if m.TagData.TSUnixMilli == 0 {
		return time.Now().UnixMilli()
	}
	return m.TagData.TSUnixMilli
}
