package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/xact-iot/xact/rtdb/tree"
	"maps"
	"regexp"
	"strings"
	"sync"
)

// LifecycleStore persists retirement decisions separately from the removed tree.
type LifecycleStore interface {
	SaveConfig(context.Context, string, string, json.RawMessage) error
	LoadConfig(context.Context, string, string) (json.RawMessage, error)
}

var deviceToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateIngestRequest(subject string, evt IngestEvent) error {
	prefix := IngestRequestSubject
	if evt.Operation == "delete" {
		prefix = DeleteRequestSubject
	} else if evt.Operation != "" && evt.Operation != "upsert" {
		return fmt.Errorf("unsupported device operation")
	}
	for _, token := range append([]string{evt.Tenant, evt.DeviceName}, strings.Split(evt.DeviceType, ".")...) {
		if !deviceToken.MatchString(token) {
			return fmt.Errorf("invalid device address")
		}
	}
	if evt.Zone != "" && !deviceToken.MatchString(evt.Zone) {
		return fmt.Errorf("invalid zone")
	}
	if subject != IngestSubjectFor(prefix, evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName) {
		return fmt.Errorf("device address or operation does not match authorized subject")
	}
	return nil
}

func (p *Processor) SetLifecycleStore(store LifecycleStore) { p.lifecycleStore = store }

func lifecycleKey(zone, typ, name string) string {
	relative := typ + "." + name
	if zone != "" {
		relative = zone + "." + relative
	}
	return retirementKey(relative)
}
func retirementKey(relative string) string {
	h := sha256.Sum256([]byte(relative))
	return "ingest_retired_" + hex.EncodeToString(h[:])
}

// Retirement follows a session, so a stable device name can be reused safely.
// Legacy unversioned deletes remain permanent path retirements.
type deviceLifecycle struct {
	Active    string          `json:"active_session,omitempty"`
	Retired   map[string]bool `json:"retired_sessions,omitempty"`
	Permanent bool            `json:"permanent,omitempty"`
	Session   string          `json:"session,omitempty"` // old persisted retirement format
}

func decodeLifecycle(raw json.RawMessage) (deviceLifecycle, error) {
	state := deviceLifecycle{}
	if len(raw) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Session != "" {
		state.Retired = map[string]bool{state.Session: true}
		state.Session = ""
	} else if state.Active == "" && len(state.Retired) == 0 {
		state.Permanent = true
	}
	return state, nil
}

func (p *Processor) loadLifecycle(tenant, zone, typ, name string) (deviceLifecycle, error) {
	key := tenant + "/" + lifecycleKey(zone, typ, name)
	if cached, ok := p.retired.Load(key); ok {
		return cached.(deviceLifecycle), nil
	}
	state := deviceLifecycle{}
	if p.lifecycleStore != nil {
		raw, err := p.lifecycleStore.LoadConfig(context.Background(), tenant, lifecycleKey(zone, typ, name))
		if err != nil {
			return state, err
		}
		state, err = decodeLifecycle(raw)
		if err != nil {
			return state, err
		}
	}
	p.retired.Store(key, state)
	return state, nil
}
func (p *Processor) saveLifecycle(evt IngestEvent, state deviceLifecycle) error {
	if p.lifecycleStore != nil {
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if err = p.lifecycleStore.SaveConfig(context.Background(), evt.Tenant, lifecycleKey(evt.Zone, evt.DeviceType, evt.DeviceName), raw); err != nil {
			return err
		}
	}
	p.retired.Store(evt.Tenant+"/"+lifecycleKey(evt.Zone, evt.DeviceType, evt.DeviceName), state)
	return nil
}
func (p *Processor) deviceRetired(tenant, zone, typ, name string) (bool, error) {
	state, err := p.loadLifecycle(tenant, zone, typ, name)
	return state.Permanent || state.Active != "" || len(state.Retired) > 0, err
}
func (p *Processor) deviceSession(path string) string {
	leaf, err := p.treeOps.FindLeaf(path + ".meta.session")
	if err != nil {
		return ""
	}
	value, _ := leaf.GetAnyValue().(string)
	return value
}

// ProcessEvent runs in the same partition queue for upserts and deletion.
func (p *Processor) ProcessEvent(evt IngestEvent) error {
	if evt.Operation != "" && evt.Operation != "upsert" && evt.Operation != "delete" {
		return fmt.Errorf("unsupported device operation")
	}
	if evt.Operation != "delete" && evt.Session == "" {
		return p.WriteDeviceData(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName, evt.TagData)
	}
	lock := p.lifecycleLock(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	lock.Lock()
	defer lock.Unlock()
	state, err := p.loadLifecycle(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	if err != nil {
		return err
	}
	path := DevicePath(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	currentSession := p.deviceSession(path)
	if state.Active == "" && currentSession != "" && !state.Permanent && !state.Retired[currentSession] {
		state.Active = currentSession
	}
	if evt.Operation == "delete" {
		if evt.Session == "" {
			if state.Active != "" {
				return fmt.Errorf("session required to retire a versioned device")
			}
			state.Permanent = true
		} else {
			state.Retired = maps.Clone(state.Retired)
			if state.Retired == nil {
				state.Retired = map[string]bool{}
			}
			state.Retired[evt.Session] = true
			if state.Active == evt.Session {
				state.Active = ""
			}
		}
		if err = p.saveLifecycle(evt, state); err != nil {
			return err
		}
		// A retry or delayed delete for an older trip cannot remove a newer trip.
		if evt.Session != "" && (state.Active != "" || (currentSession != "" && currentSession != evt.Session)) {
			return nil
		}
		if _, err = p.treeOps.FindNode(path); err != nil {
			return nil
		}
		return DeleteDevice(p.treeOps, evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	}
	if state.Permanent || state.Retired[evt.Session] {
		return fmt.Errorf("device session has retired")
	}
	if meta := evt.TagData.Groups["meta"]; meta != nil {
		if session, ok := meta["session"]; ok && session != evt.Session {
			return fmt.Errorf("session does not match device metadata")
		}
	}
	if state.Active != evt.Session {
		state.Retired = maps.Clone(state.Retired)
		if state.Retired == nil {
			state.Retired = map[string]bool{}
		}
		if state.Active != "" {
			state.Retired[state.Active] = true
		}
		if currentSession != "" && currentSession != evt.Session {
			state.Retired[currentSession] = true
		}
		state.Active = evt.Session
		if err = p.saveLifecycle(evt, state); err != nil {
			return err
		}
	}
	if evt.TagData.Groups == nil {
		evt.TagData.Groups = map[string]map[string]any{}
	} else {
		evt.TagData.Groups = maps.Clone(evt.TagData.Groups)
	}
	meta := maps.Clone(evt.TagData.Groups["meta"])
	if meta == nil {
		meta = map[string]any{}
	}
	meta["session"] = evt.Session
	evt.TagData.Groups["meta"] = meta
	return p.writeDeviceData(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName, evt.TagData)
}

func (p *Processor) lifecycleLock(tenant, zone, typ, name string) *sync.Mutex {
	h := sha256.Sum256([]byte(DevicePath(tenant, zone, typ, name)))
	return &p.lifecycleLocks[int(h[0])%len(p.lifecycleLocks)]
}

// ReconcileRetiredDevices removes nodes restored from a tree snapshot saved
// before an acknowledged delete. The decision store is authoritative.
func (p *Processor) ReconcileRetiredDevices() error {
	if p.lifecycleStore == nil {
		return nil
	}
	var walk func(*tree.Node, string) error
	walk = func(node *tree.Node, path string) error {
		if node.GetNodeType() == tree.NodeTypeDevice {
			parts := strings.SplitN(path, ".", 2)
			if len(parts) != 2 {
				return nil
			}
			raw, err := p.lifecycleStore.LoadConfig(context.Background(), parts[0], retirementKey(parts[1]))
			if err != nil {
				return err
			}
			if len(raw) > 0 {
				state, err := decodeLifecycle(raw)
				if err != nil {
					return err
				}
				p.retired.Store(parts[0]+"/"+retirementKey(parts[1]), state)
				session := p.deviceSession(path)
				if state.Permanent || state.Active == "" || state.Retired[session] {
					return p.treeOps.DeleteNode(path)
				}
			}
			return nil
		}
		for name, child := range node.GetChildren() {
			if n, ok := child.(*tree.Node); ok {
				next := name
				if path != "" {
					next = path + "." + name
				}
				if err := walk(n, next); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(p.treeOps.Root, "")
}
