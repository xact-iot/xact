package nats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/xact-iot/xact/rtdb/tree"
)

const tagBatchTargetBytes = 256 * 1024

// Values is the complete latest snapshot for this retained subject. Changed
// identifies the entries to dispatch on a live subscription, so replay data
// does not retrigger scripts or callbacks for unchanged tags.
type TagValueBatch struct {
	Values  map[string]tree.TagValue `json:"values"`
	Changed []string                 `json:"changed"`
}

type tagPublicationState struct {
	mu            sync.Mutex
	groups        map[string]*tagPublicationGroup
	scopes        map[string]*tagPublicationScope
	active        map[string]*tagPublicationScope
	lastForgotten string
}

type tagPublicationScope struct {
	gate    sync.Mutex
	pending sync.WaitGroup
	active  bool
	groups  map[string]*tagPublicationGroup
}

type tagPublicationGroup struct {
	mu       sync.Mutex
	path     string
	chunks   map[string]*tagPublicationChunk
	obsolete []string
	owners   map[*tagPublicationScope]bool
}

type tagPublicationChunk struct {
	values  map[string]tree.TagValue
	changed map[string]bool
	dirty   bool
}

func newTagChunk() *tagPublicationChunk {
	return &tagPublicationChunk{values: map[string]tree.TagValue{}, changed: map[string]bool{}}
}

func (s *tagPublicationState) group(path string) *tagPublicationGroup {
	if s.groups == nil {
		s.groups = map[string]*tagPublicationGroup{}
	}
	g := s.groups[path]
	if g == nil {
		g = &tagPublicationGroup{path: path, chunks: map[string]*tagPublicationChunk{"": newTagChunk()}}
		s.groups[path] = g
	}
	return g
}

func normalizeTagPath(path string) string {
	return strings.TrimPrefix(normalizeTreePath(path), "tagvalue.")
}

func normalizeTreePath(path string) string { return strings.Trim(SubjectForPath(path), ".") }

func pathWithin(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+".")
}

// Ingest already serializes each device's lifecycle. The scope also serializes
// overlapping callers and flushes before the ingest acknowledgement, without
// a timer that could publish after deletion or a session change.
func (b *BroadcastStream) BeginTagValueBatch(devicePath string) func() error {
	path := normalizeTreePath(devicePath)
	b.tags.mu.Lock()
	if b.tags.scopes == nil {
		b.tags.scopes = map[string]*tagPublicationScope{}
	}
	scope := b.tags.scopes[path]
	if scope == nil {
		scope = &tagPublicationScope{groups: map[string]*tagPublicationGroup{}}
		b.tags.scopes[path] = scope
	}
	b.tags.mu.Unlock()
	scope.gate.Lock()
	b.tags.mu.Lock()
	scope.active = true
	if b.tags.active == nil {
		b.tags.active = map[string]*tagPublicationScope{}
	}
	b.tags.active[path] = scope
	b.tags.mu.Unlock()
	return func() error {
		defer scope.gate.Unlock()
		b.tags.mu.Lock()
		scope.active = false
		delete(b.tags.active, path)
		groups := make([]*tagPublicationGroup, 0, len(scope.groups))
		for _, g := range scope.groups {
			groups = append(groups, g)
		}
		if len(groups) == 0 && b.tags.scopes[path] == scope {
			delete(b.tags.scopes, path)
		}
		b.tags.mu.Unlock()
		scope.pending.Wait()
		// Child arrays are published before their complete parent snapshots.
		sort.Slice(groups, func(i, j int) bool { return groups[i].path > groups[j].path })
		var first error
		for _, g := range groups {
			if err := b.flushTagGroup(g); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
}

func (b *BroadcastStream) PublishTagValue(tagPath string, value tree.TagValue) error {
	// A complete array and its elements are committed together by ingest. The
	// transient start marker must not overwrite a retained latest value.
	if value.Type == "array-start" {
		return nil
	}
	path := normalizeTagPath(tagPath)
	index := strings.LastIndexByte(path, '.')
	if index <= 0 || index == len(path)-1 {
		return fmt.Errorf("invalid tag path %q", path)
	}
	b.tags.mu.Lock()
	if pathWithin(path, b.tags.lastForgotten) {
		b.tags.lastForgotten = ""
	}
	g := b.tags.group(path[:index])
	var scope *tagPublicationScope
	longest := 0
	for prefix, candidate := range b.tags.active {
		if candidate.active && len(prefix) > longest && pathWithin(path, prefix) {
			scope, longest = candidate, len(prefix)
		}
	}
	if scope != nil {
		scope.groups[g.path] = g
		if g.owners == nil {
			g.owners = map[*tagPublicationScope]bool{}
		}
		g.owners[scope] = true
		scope.pending.Add(1)
	}
	b.tags.mu.Unlock()
	g.mu.Lock()
	g.put(path, value, true)
	g.mu.Unlock()
	if scope != nil {
		scope.pending.Done()
		return nil
	}
	return b.flushTagGroup(g)
}

// SeedTagValues keeps explicitly restored values in later group snapshots.
// Undefined leaves are not made visible merely because their config exists.
func (b *BroadcastStream) SeedTagValues(ops *tree.TreeWithOperations) {
	ops.WalkLeaves(func(path string, leaf tree.Leaf) {
		updated, status := leaf.GetUpdatedTime(), leaf.GetState()
		if updated.IsZero() || strings.Contains(status, tree.StatusUndefined) {
			return
		}
		publish := false
		for _, block := range leaf.GetPipeline() {
			if block.GetType() == "publish" {
				publish = true
				break
			}
		}
		if !publish {
			return
		}
		path = normalizeTreePath(path)
		index := strings.LastIndexByte(path, '.')
		if index <= 0 {
			return
		}
		b.tags.mu.Lock()
		g := b.tags.group(path[:index])
		g.mu.Lock()
		g.put(path, tree.TagValue{Type: "value", Value: leaf.GetAnyValue(), Status: status, Timestamp: updated.UnixMilli()}, false)
		g.mu.Unlock()
		b.tags.mu.Unlock()
	})
}

func tagPartitionKey(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])
}

func (g *tagPublicationGroup) put(path string, value tree.TagValue, dirty bool) {
	key := tagPartitionKey(path)
	for depth := 0; depth <= len(key); depth++ {
		if chunk := g.chunks[key[:depth]]; chunk != nil {
			chunk.values[path] = value
			if dirty {
				chunk.changed[path], chunk.dirty = true, true
			}
			return
		}
	}
	panic("tag batch partition is not routable")
}

func tagBatchSubject(group, partition string) string {
	if partition == "" {
		partition = "all"
	}
	return BroadcastStreamPrefix + string(TagBatchStream) + "." + group + "." + partition
}

func encodeTagChunk(chunk *tagPublicationChunk) ([]byte, error) {
	changed := make([]string, 0, len(chunk.changed))
	for path := range chunk.changed {
		changed = append(changed, path)
	}
	sort.Strings(changed)
	return json.Marshal(TagValueBatch{Values: chunk.values, Changed: changed})
}

func (b *BroadcastStream) flushTagGroup(g *tagPublicationGroup) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := NewPubLock(SubjectName(g.path)).TryLock(); err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			return nil
		}
		return err
	}
	for {
		split := false
		for prefix, chunk := range g.chunks {
			if !chunk.dirty {
				continue
			}
			data, err := encodeTagChunk(chunk)
			if err != nil {
				return err
			}
			if len(data) <= tagBatchTargetBytes || len(chunk.values) == 1 {
				continue
			}
			delete(g.chunks, prefix)
			for _, digit := range "0123456789abcdef" {
				g.chunks[prefix+string(digit)] = newTagChunk()
			}
			for path, value := range chunk.values {
				childPrefix := tagPartitionKey(path)[:len(prefix)+1]
				child := g.chunks[childPrefix]
				child.values[path], child.dirty = value, true
				if chunk.changed[path] {
					child.changed[path] = true
				}
			}
			g.obsolete = append(g.obsolete, tagBatchSubject(g.path, prefix))
			split = true
			break
		}
		if !split {
			break
		}
	}
	keys := make([]string, 0, len(g.chunks))
	for prefix := range g.chunks {
		keys = append(keys, prefix)
	}
	sort.Strings(keys)
	for _, prefix := range keys {
		chunk := g.chunks[prefix]
		if !chunk.dirty {
			continue
		}
		if len(chunk.values) == 0 {
			if err := b.purgeTagBatch(tagBatchSubject(g.path, prefix)); err != nil {
				return err
			}
		} else {
			data, err := encodeTagChunk(chunk)
			if err != nil {
				return err
			}
			if _, err := b.js.Publish(context.Background(), tagBatchSubject(g.path, prefix), data); err != nil {
				return err
			}
		}
		chunk.dirty = false
		clear(chunk.changed)
	}
	// Publish every replacement before removing the old retained partition.
	for len(g.obsolete) > 0 {
		if err := b.purgeTagBatch(g.obsolete[0]); err != nil {
			return err
		}
		g.obsolete = g.obsolete[1:]
	}
	return nil
}

func (b *BroadcastStream) purgeTagBatch(subject string) error {
	stream, err := b.js.Stream(context.Background(), string(TagValueStream))
	if err != nil {
		return err
	}
	return stream.Purge(context.Background(), jetstream.WithPurgeSubject(subject))
}

// ForgetTagValues removes deleted paths from both the in-memory group snapshot
// and its retained messages. It is called by the tree deletion callback.
func (b *BroadcastStream) ForgetTagValues(path string) error {
	path = normalizeTreePath(path)
	b.tags.mu.Lock()
	// Tree deletion reports the parent and then all its descendants. The first
	// callback has already removed those values; avoid a full cache scan per leaf.
	if b.tags.lastForgotten != "" && pathWithin(path, b.tags.lastForgotten) {
		b.tags.mu.Unlock()
		return nil
	}
	b.tags.lastForgotten = path
	var groups []*tagPublicationGroup
	for groupPath, g := range b.tags.groups {
		if !pathWithin(groupPath, path) && !pathWithin(path, groupPath) {
			continue
		}
		g.mu.Lock()
		for _, chunk := range g.chunks {
			for tag := range chunk.values {
				if pathWithin(tag, path) {
					delete(chunk.values, tag)
					delete(chunk.changed, tag)
					chunk.dirty = true
				}
			}
		}
		g.mu.Unlock()
		groups = append(groups, g)
		if pathWithin(groupPath, path) {
			delete(b.tags.groups, groupPath)
			for scope := range g.owners {
				delete(scope.groups, groupPath)
			}
			clear(g.owners)
		}
	}
	for scopePath := range b.tags.scopes {
		if pathWithin(scopePath, path) {
			delete(b.tags.scopes, scopePath)
		}
	}
	b.tags.mu.Unlock()
	var first error
	for _, g := range groups {
		if err := b.flushTagGroup(g); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		b.tags.mu.Lock()
		if b.tags.lastForgotten == path {
			b.tags.lastForgotten = ""
		}
		b.tags.mu.Unlock()
	}
	return first
}
