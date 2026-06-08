package nats

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Common stream subjects and kv keys
// These are prepended with the broadcastStreamPrefix and workerQueuePrefix as appropriate
type SubjectName string

const (
	TagValueStream SubjectName = "tagvalue"
)

// Publisher
//
// Used when a message must be sent as result of processing of incoming values. With distributed server instances
// only one must publish. The NATS cliuster leader is the one that publishes
// Two types of publish are supported
// 1. Broadcast. Typically informing all the browsers of an update
const BroadcastStreamPrefix = "xact.internal.bcast."

var natsConn *nats.Conn

var broadcastStreamRegistry sync.Map
var workerQueueRegistry sync.Map

// Broadcast Stream
// ------------------------------
type BroadcastStream struct {
	js jetstream.JetStream
}

// name is the subclass of messages, e.g. TagValue
func GetBroadcastStream(name SubjectName) (*BroadcastStream, error) {
	b, ok := broadcastStreamRegistry.Load(name)
	if ok {
		return b.(*BroadcastStream), nil
	}
	bcast := &BroadcastStream{}
	var err error
	bcast.js, err = jetstream.New(natsConn)
	if err != nil {
		log.Printf("NATS Jetstream create error %v", err)
		return bcast, err
	}

	cfg := jetstream.StreamConfig{
		Name:              string(name),
		Subjects:          []string{BroadcastStreamPrefix + string(name) + ".>"},
		Retention:         jetstream.LimitsPolicy,
		MaxAge:            24 * time.Hour,
		Storage:           jetstream.MemoryStorage,
		MaxMsgsPerSubject: 1,
	}

	_, err = bcast.js.CreateOrUpdateStream(context.Background(), cfg)
	if err != nil {
		return bcast, err
	}
	broadcastStreamRegistry.Store(name, bcast)
	return bcast, err
}
func (bcast *BroadcastStream) TagValuePublish(tagPath string, data []byte) error {
	_, err := bcast.Publish(tagPath, data, 0)
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}
	return nil
}

// Publish publishes data for the given path.
//
// lastRevision is the revision returned by the previous successful Publish call
// for this path (0 on first call).  It is used to prove lock ownership when the
// lock key already exists, allowing the same server to publish multiple updates
// within a single TTL window without letting a delayed peer sneak in.
//
// Returns the new lock revision on success, or 0 if this server was deduplicated
// (another server holds the lock and the caller's revision was stale).
// A zero return with a nil error is not an error - the caller should simply not
// update its saved revision.
func (bcast *BroadcastStream) Publish(path string, data []byte, lastRevision uint64) (uint64, error) {
	subject := SubjectForPath(path)
	pathLock := NewPubLock(SubjectName(path))

	rev, err := pathLock.TryLock()
	if errors.Is(err, jetstream.ErrKeyExists) {
		// Lock is held - attempt to refresh using our saved revision (CAS).
		// Only the server that last set the lock knows the current revision, so
		// only it can succeed here.  A delayed peer with a stale revision fails
		// and its publish is suppressed.
		rev, err = pathLock.RefreshLock(lastRevision)
		if err != nil {
			log.Printf("[Publisher] refresh lock '%s' failed %v\n", SubjectName(path), err)
			return 0, nil // deduplicated - another server owns the lock
		}
	} else if err != nil {
		log.Printf("[Publisher] aquire lock '%s' error %v\n", SubjectName(path), err)
		return 0, err
	}
	if _, err = bcast.js.Publish(context.Background(), BroadcastStreamPrefix+subject, data); err != nil {
		log.Printf("[Publish] Publish '%s' error %v\n", SubjectName(path), err)
		return 0, err
	}
	return rev, nil
}
func (bcast *BroadcastStream) Close(path string, lastRevision uint64) {
	pathLock := NewPubLock(SubjectName(path))
	pathLock.Release(lastRevision)
}

// Worker Queue
// -----------------------------
type WorkerQ struct {
	js   jetstream.JetStream
	lock PubLock
}

func GetWorkerQ(name SubjectName) (*WorkerQ, error) {
	w, ok := workerQueueRegistry.Load(name)
	if ok {
		return w.(*WorkerQ), nil
	}
	worker := &WorkerQ{}
	var err error
	worker.js, err = jetstream.New(natsConn)
	if err != nil {
		log.Printf("NATS Jetstream create error %v", err)
		return nil, err
	}

	worker.lock = NewPubLock(name)

	cfg := jetstream.StreamConfig{
		Name:              string(name),
		Subjects:          []string{BroadcastStreamPrefix + string(name) + ".>"},
		Retention:         jetstream.WorkQueuePolicy,
		MaxAge:            24 * time.Hour,
		Storage:           jetstream.MemoryStorage,
		MaxMsgsPerSubject: 10,
	}

	_, err = worker.js.CreateStream(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	workerQueueRegistry.Store(name, worker)
	return worker, err
}
func (w *WorkerQ) Publish(subject SubjectName, data []byte) error {
	rev, err := w.lock.TryLock()
	if errors.Is(err, jetstream.ErrKeyExists) {
		return nil
	}
	if err != nil {
		return err
	}
	defer w.lock.Release(rev)
	_, err = w.js.Publish(context.Background(), BroadcastStreamPrefix+string(subject)+".>", data)
	return err
}

// Convert tag path to NATS subject
func SubjectForPath(path string) string {
	dotPath := strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", ".")
	return dotPath
}
