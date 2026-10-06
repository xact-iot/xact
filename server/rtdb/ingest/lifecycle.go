package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/xact-iot/xact/rtdb/tree"
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

func (p *Processor) deviceRetired(tenant, zone, typ, name string) (bool, error) {
	key := tenant + "/" + lifecycleKey(zone, typ, name)
	if cached, ok := p.retired.Load(key); ok {
		return cached.(bool), nil
	}
	retired := false
	if p.lifecycleStore != nil {
		raw, err := p.lifecycleStore.LoadConfig(context.Background(), tenant, lifecycleKey(zone, typ, name))
		if err != nil {
			return false, err
		}
		retired = len(raw) > 0
	}
	p.retired.Store(key, retired)
	return retired, nil
}

// ProcessEvent runs in the same partition queue for upserts and deletion.
// A retired device path is never reused; a new session gets a new device name.
func (p *Processor) ProcessEvent(evt IngestEvent) error {
	if evt.Operation != "delete" {
		if evt.Operation != "" && evt.Operation != "upsert" {
			return fmt.Errorf("unsupported device operation")
		}
		return p.WriteDeviceData(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName, evt.TagData)
	}
	lock := p.lifecycleLock(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	lock.Lock()
	defer lock.Unlock()
	key := lifecycleKey(evt.Zone, evt.DeviceType, evt.DeviceName)
	if p.lifecycleStore != nil {
		raw, _ := json.Marshal(map[string]string{"session": evt.Session, "device_type": evt.DeviceType, "device_name": evt.DeviceName})
		if err := p.lifecycleStore.SaveConfig(context.Background(), evt.Tenant, key, raw); err != nil {
			return err
		}
	}
	p.retired.Store(evt.Tenant+"/"+key, true)
	path := DevicePath(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	if _, err := p.treeOps.FindNode(path); err != nil {
		return nil
	}
	return DeleteDevice(p.treeOps, evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
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
				p.retired.Store(parts[0]+"/"+retirementKey(parts[1]), true)
				return p.treeOps.DeleteNode(path)
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
