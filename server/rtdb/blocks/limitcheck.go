package blocks

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xact-iot/xact/events"
	xnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
)

func init() {
	tree.RegisterProcessBlock("limitcheck", func() tree.ProcessBlock { return &LimitCheckBlock{} })
}

func derefPtrFloat64(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *p)
}

// EventConfig holds the event parameters for a single alarm transition.
type EventConfig struct {
	Enabled        bool   `json:"enabled"`
	Severity       string `json:"severity"` // DEBUG | INFO | WARN | ERROR | CRITICAL
	Message        string `json:"message"`
	NotificationID int    `json:"notificationId"` // 0 = no notification profile
}

// LimitCheckBlock checks value against hi/lo limits and sets tag state to InAlarm.
// On state transitions it emits events via the events package, using a distributed
// lock to ensure at-most-once delivery in clustered deployments.
type LimitCheckBlock struct {
	HiLimit  *float64    `json:"hiLimit,omitempty"`
	LowLimit *float64    `json:"lowLimit,omitempty"`
	HiEvent  EventConfig `json:"hiEvent"`
	RtnEvent EventConfig `json:"rtnEvent"` // Return to Normal
	LowEvent EventConfig `json:"lowEvent"`

	// runtime state - not serialised. Template-linked leaves share this block,
	// so alarm transitions and deadband state are tracked per leaf.
	mu      sync.Mutex
	states  map[string]*limitLeafState
	hiLock  xnats.PubLock
	rtnLock xnats.PubLock
	lowLock xnats.PubLock
}

type limitLeafState struct {
	leaf       tree.Leaf
	wasInAlarm bool
	lastValue  *float64
}

func (b *LimitCheckBlock) GetType() string { return "limitcheck" }

func (b *LimitCheckBlock) Init(leaf tree.Leaf) {
	if leaf == nil {
		return
	}
	path := tagPath(leaf)
	b.hiLock = xnats.NewPubLock(xnats.SubjectName(path + ".alarm.hi"))
	b.rtnLock = xnats.NewPubLock(xnats.SubjectName(path + ".alarm.rtn"))
	b.lowLock = xnats.NewPubLock(xnats.SubjectName(path + ".alarm.low"))
	b.initializeState(leaf)
}

func (b *LimitCheckBlock) Close(leaf tree.Leaf) {
}

func (b *LimitCheckBlock) Process(leaf tree.Leaf, value any) (any, error) {
	v, err := toFloat64(value)
	if err != nil {
		return value, nil // pass through non-numeric
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.stateLocked(leaf)
	if state.withinDeadband(leaf, v) {
		return value, nil
	}

	hiBreached := b.HiLimit != nil && v > *b.HiLimit
	lowBreached := b.LowLimit != nil && v < *b.LowLimit
	inAlarm := hiBreached || lowBreached

	// log.Printf("limitcheck.Process: value=%v, hiLimit=%v, loLimit=%v, hiBreached=%v, lowBreached=%v, wasInAlarm=%v",
	// 	v, derefPtrFloat64(b.HiLimit), derefPtrFloat64(b.LowLimit), hiBreached, lowBreached, b.wasInAlarm)

	if leaf != nil {
		if inAlarm {
			leaf.SetState(tree.StatusAlarm)
		} else {
			leaf.ClearState(tree.StatusAlarm)
		}
	}

	if eventsPublisher != nil && leaf != nil {
		orgName := leaf.GetTreeOrg()
		device := leaf.GetTreeDevice()
		units := leaf.GetShared().Units
		switch {
		case !state.wasInAlarm && hiBreached:
			log.Printf("limitcheck: hi breach transition, emitting event")
			b.emitEvent(&b.hiLock, b.HiEvent, orgName, device, v, units)
		case !state.wasInAlarm && lowBreached:
			log.Printf("limitcheck: lo breach transition, emitting event")
			b.emitEvent(&b.lowLock, b.LowEvent, orgName, device, v, units)
		case state.wasInAlarm && !inAlarm:
			log.Printf("limitcheck: return to normal, emitting event")
			b.emitEvent(&b.rtnLock, b.RtnEvent, orgName, device, v, units)
		}
	}

	state.wasInAlarm = inAlarm
	state.lastValue = &v
	return value, nil // pass value through unchanged
}

func (b *LimitCheckBlock) initializeState(leaf tree.Leaf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stateLocked(leaf)
}

// stateLocked returns the transition state for leaf. A leaf recreated at the
// same path (for example a device deleted and provisioned again) starts from
// its own alarm state rather than inheriting the previous leaf's.
func (b *LimitCheckBlock) stateLocked(leaf tree.Leaf) *limitLeafState {
	key := ""
	if leaf != nil {
		key = tagPath(leaf)
	}
	if state := b.states[key]; state != nil && state.leaf == leaf {
		return state
	}
	state := &limitLeafState{leaf: leaf}
	if leaf != nil {
		state.wasInAlarm = strings.Contains(leaf.GetState(), tree.StatusAlarm)
		if !leaf.GetUpdatedTime().IsZero() {
			if v, err := toFloat64(leaf.GetAnyValue()); err == nil {
				state.lastValue = &v
			}
		}
	}
	if b.states == nil {
		b.states = map[string]*limitLeafState{}
	}
	b.states[key] = state
	return state
}

func (s *limitLeafState) withinDeadband(leaf tree.Leaf, value float64) bool {
	if leaf == nil || s.lastValue == nil {
		return false
	}
	deadband := leaf.GetShared().Deadband
	if deadband <= 0 {
		return false
	}
	return math.Abs(value-*s.lastValue) <= deadband
}

// emitEvent publishes an event using the distributed lock to deduplicate across
// clustered instances.
func (b *LimitCheckBlock) emitEvent(lock *xnats.PubLock, cfg EventConfig, orgName, device string, value float64, units string) {
	log.Printf("limitcheck.emitEvent: org=%s, device=%s, enabled=%v, lock=%v", orgName, device, cfg.Enabled, lock != nil)
	if !cfg.Enabled || lock == nil {
		log.Printf("limitcheck.emitEvent: skipped - enabled=%v, lock=%v", cfg.Enabled, lock != nil)
		return
	}
	rev, err := lock.TryLock()
	if err != nil {
		log.Printf("limitcheck.emitEvent: lock acquisition failed, another instance handling")
		return // another server instance is handling this transition
	}
	defer lock.Release(rev)

	msg := cfg.Message
	if msg == "" {
		msg = "Alarm condition"
	}
	severity := events.Severity(cfg.Severity)
	if severity == "" {
		severity = events.Info
	}
	params := []any{"value", value}
	if units != "" {
		params = append(params, "units", units)
	}
	events.PublishEvent(eventsPublisher, time.Now(), orgName, device, 0, severity, cfg.NotificationID, msg, params...)
}

func (b *LimitCheckBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *LimitCheckBlock) SetParameters(data json.RawMessage) error {
	if err := json.Unmarshal(data, b); err != nil {
		return err
	}
	return nil
}

var severityOptions = []string{
	string(events.Debug), string(events.Info), string(events.Warn), string(events.Error), string(events.Critical),
}

func (b *LimitCheckBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "limitcheck",
		Label:       "Limit Check",
		Description: "Sets ALARM status when value exceeds hi/lo limits; emits events on transitions",
		Params: map[string]tree.ParamSchema{
			"hiLimit":                 {Type: "number", Label: "High Limit"},
			"lowLimit":                {Type: "number", Label: "Low Limit"},
			"hiEvent.enabled":         {Type: "boolean", Label: "Hi Alarm Event Enabled"},
			"hiEvent.severity":        {Type: "select", Label: "Hi Alarm Severity", Options: severityOptions, Default: string(events.Warn)},
			"hiEvent.message":         {Type: "string", Label: "Hi Alarm Message"},
			"hiEvent.notificationId":  {Type: "notification-profile", Label: "Hi Alarm Notification"},
			"rtnEvent.enabled":        {Type: "boolean", Label: "Return to Normal Event Enabled"},
			"rtnEvent.severity":       {Type: "select", Label: "Return to Normal Severity", Options: severityOptions, Default: string(events.Info)},
			"rtnEvent.message":        {Type: "string", Label: "Return to Normal Message"},
			"rtnEvent.notificationId": {Type: "notification-profile", Label: "Return to Normal Notification"},
			"lowEvent.enabled":        {Type: "boolean", Label: "Low Alarm Event Enabled"},
			"lowEvent.severity":       {Type: "select", Label: "Low Alarm Severity", Options: severityOptions, Default: string(events.Warn)},
			"lowEvent.message":        {Type: "string", Label: "Low Alarm Message"},
			"lowEvent.notificationId": {Type: "notification-profile", Label: "Low Alarm Notification"},
		},
	}
}
