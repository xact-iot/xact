package blocks

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/xact-iot/xact/events"
	xnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
)

func init() {
	tree.RegisterProcessBlock("flapfilter", func() tree.ProcessBlock { return &FlapFilterBlock{} })
}

// FlapFilterBlock detects rapid alarm-state oscillation on a leaf and
// suppresses further toggling until the state stabilises. It monitors the
// leaf's StatusAlarm flag; when it observes more than Threshold transitions
// within the Window period it enters the flapping state.
//
// While flapping the alarm flag is locked to its current value (upstream
// set/clear is reversed), StatusDeviation is set, and an optional event is
// emitted. Values always pass through unchanged.
//
// Must be placed after the limitcheck block in the pipeline.
type FlapFilterBlock struct {
	Threshold int           `json:"threshold"`
	Window    time.Duration `json:"window"`
	Cooldown  time.Duration `json:"cooldown"`
	FlapEvent EventConfig   `json:"flapEvent"`
	RtnEvent  EventConfig   `json:"rtnEvent"`

	// runtime state - not serialised
	mu              sync.Mutex
	lastAlarmState  string // last observed alarm component of leaf state
	transitions     []time.Time
	flapping        bool
	lockedAlarm     bool   // whether alarm was active when flapping started
	lockedAlarmFlag string // the StatusAlarm string locked at flapping onset
	cooldownTimer   *tree.Timer
	flapLock        xnats.PubLock
	rtnLock         xnats.PubLock
}

func (b *FlapFilterBlock) GetType() string { return "flapfilter" }

func (b *FlapFilterBlock) Init(leaf tree.Leaf) {
	if b.Threshold == 0 {
		b.Threshold = 1
	}
	if b.Window == 0 {
		b.Window = 60 * time.Second
	}
	if b.Cooldown == 0 {
		b.Cooldown = 120 * time.Second
	}

	if leaf != nil {
		path := tagPath(leaf)
		b.flapLock = xnats.NewPubLock(xnats.SubjectName(path + ".flap.start"))
		b.rtnLock = xnats.NewPubLock(xnats.SubjectName(path + ".flap.rtn"))
		b.lastAlarmState = alarmComponent(leaf.GetState())
	}
}

func (b *FlapFilterBlock) Close(leaf tree.Leaf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cooldownTimer != nil {
		b.cooldownTimer.Stop()
		b.cooldownTimer = nil
	}
}

func (b *FlapFilterBlock) Process(leaf tree.Leaf, value any) (any, error) {
	if leaf == nil {
		return value, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	currentAlarm := alarmComponent(leaf.GetState())
	now := time.Now()

	// Detect alarm state transition.
	if currentAlarm != b.lastAlarmState {
		b.transitions = append(b.transitions, now)
		b.lastAlarmState = currentAlarm

		if b.flapping {
			// Reset cooldown timer on each new transition while flapping.
			b.resetCooldown(leaf)
		}
	}

	// Prune transitions outside the window.
	cutoff := now.Add(-b.Window)
	b.transitions = pruneOlderThan(b.transitions, cutoff)

	if !b.flapping {
		// Check if we should enter flapping state.
		if len(b.transitions) > b.Threshold {
			b.enterFlapping(leaf, currentAlarm)
		}
	}

	if b.flapping {
		// Lock alarm flag: reverse whatever the upstream limitcheck did.
		if b.lockedAlarm {
			leaf.SetState(tree.StatusAlarm)
		} else {
			leaf.ClearState(tree.StatusAlarm)
		}
	}

	return value, nil
}

// enterFlapping transitions into the flapping state.
func (b *FlapFilterBlock) enterFlapping(leaf tree.Leaf, currentAlarm string) {
	b.flapping = true
	b.lockedAlarm = currentAlarm != ""
	leaf.SetState(tree.StatusDeviation)
	b.emitEvent(&b.flapLock, b.FlapEvent, leaf)
	b.resetCooldown(leaf)
}

// exitFlapping transitions out of the flapping state.
func (b *FlapFilterBlock) exitFlapping(leaf tree.Leaf) {
	b.flapping = false
	b.transitions = b.transitions[:0]
	leaf.ClearState(tree.StatusDeviation)
	b.emitEvent(&b.rtnLock, b.RtnEvent, leaf)
}

// resetCooldown (re)starts the cooldown timer. Must be called with mu held.
func (b *FlapFilterBlock) resetCooldown(leaf tree.Leaf) {
	if b.cooldownTimer != nil {
		b.cooldownTimer.Stop()
	}
	b.cooldownTimer = staleTimingWheel.AfterFunc(b.Cooldown, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.flapping {
			b.exitFlapping(leaf)
		}
	})
}

func (b *FlapFilterBlock) emitEvent(lock *xnats.PubLock, cfg EventConfig, leaf tree.Leaf) {
	if !cfg.Enabled || eventsPublisher == nil || lock == nil {
		return
	}
	rev, err := lock.TryLock()
	if err != nil {
		return
	}
	defer lock.Release(rev)

	msg := cfg.Message
	if msg == "" {
		msg = "Flapping condition"
	}
	severity := events.Severity(cfg.Severity)
	if severity == "" {
		severity = events.Warn
	}
	events.PublishEvent(eventsPublisher, time.Now(),
		leaf.GetTreeOrg(), leaf.GetTreeDevice(), 0,
		severity, cfg.NotificationID, msg)
}

func (b *FlapFilterBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *FlapFilterBlock) SetParameters(data json.RawMessage) error {
	return json.Unmarshal(data, b)
}

func (b *FlapFilterBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "flapfilter",
		Label:       "Flapping Filter",
		Description: "Detects rapid alarm oscillation and suppresses toggling until stable",
		Params: map[string]tree.ParamSchema{
			"threshold":                {Type: "number", Label: "Transition Threshold", Required: true, Default: 1},
			"window":                   {Type: "number", Label: "Window (sec)", Required: true, Default: 60},
			"cooldown":                 {Type: "number", Label: "Cooldown (sec)", Required: true, Default: 120},
			"flapEvent.enabled":        {Type: "boolean", Label: "Flapping Event Enabled"},
			"flapEvent.severity":       {Type: "select", Label: "Flapping Severity", Options: severityOptions, Default: string(events.Warn)},
			"flapEvent.message":        {Type: "string", Label: "Flapping Message"},
			"flapEvent.notificationId": {Type: "notification-profile", Label: "Flapping Notification"},
			"rtnEvent.enabled":         {Type: "boolean", Label: "Return to Normal Event Enabled"},
			"rtnEvent.severity":        {Type: "select", Label: "Return to Normal Severity", Options: severityOptions, Default: string(events.Info)},
			"rtnEvent.message":         {Type: "string", Label: "Return to Normal Message"},
			"rtnEvent.notificationId":  {Type: "notification-profile", Label: "Return to Normal Notification"},
		},
	}
}

// alarmComponent returns the StatusAlarm portion of a state string.
func alarmComponent(state string) string {
	if strings.Contains(state, tree.StatusAlarm) {
		return tree.StatusAlarm
	}
	return ""
}

// pruneOlderThan removes timestamps before cutoff, preserving order.
func pruneOlderThan(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for _, t := range ts {
		if !t.Before(cutoff) {
			ts[i] = t
			i++
		}
	}
	return ts[:i]
}
