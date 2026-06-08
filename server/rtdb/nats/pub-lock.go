package nats

import (
	"os"
	"strconv"
	"sync/atomic"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Publish de-duplication based on NATS server leader status
var natsServer *server.Server
var isClustered atomic.Bool

func PreparePubDedup(s *server.Server, nc *nats.Conn) error {
	natsServer = s
	natsConn = nc
	clustered, _ := strconv.ParseBool(os.Getenv("CLUSTERED"))
	isClustered.Store(clustered)
	return nil
}

type PubLock struct {
}

func NewPubLock(name SubjectName) PubLock {
	return PubLock{}
}
func (l PubLock) TryLock() (uint64, error) {
	if !isClustered.Load() {
		return 0, nil
	}
	if natsServer.JetStreamIsLeader() {
		return 0, nil
	}
	return 0, jetstream.ErrKeyExists
}
func (l PubLock) RefreshLock(_ uint64) (uint64, error) {
	return 0, nil
}
func (l PubLock) Release(_ uint64) {
}

/**
// Publish de-duplication based on NATS KV store
// The lock store is used to prevent ProcessBlocks on multiple
// server intances from creating the same side effect, e.g. publish, logging, alarm etc
// Each process block with a side effect must create their own entry in the LockStore.

var lockStore jetstream.KeyValue

// Called once on startup
func PreparePubDedup(_ *server.Server, nc *nats.Conn) error {
	natsConn = nc
	js, err := jetstream.New(nc)

	ctx := context.Background()
	lockCfg := jetstream.KeyValueConfig{
		Bucket:  "locks",
		TTL:     time.Second * 10, // Auto-expire keys after 10 seconds
		History: 1,
		Storage: jetstream.MemoryStorage,
	}

	clustered, _ := strconv.ParseBool(os.Getenv("CLUSTERED"))
	if !clustered {
		// In single-server mode, stale lock keys from the previous run can block
		// fresh Create calls until their TTL expires. Deleting and recreating the
		// bucket resets all sequences cleanly.
		if delErr := js.DeleteKeyValue(ctx, lockCfg.Bucket); delErr != nil {
			// log.Printf("PrepareLockStore: delete bucket: %v (ignored)", delErr)
		}
	}

	lockStore, err = js.CreateOrUpdateKeyValue(ctx, lockCfg)
	return err
}

type pubLock struct {
	name SubjectName
}

func NewPubLock(name SubjectName) pubLock {
	l := pubLock{}
	l.name = name
	return l
}

// Called by a process block before executing the side effect code.
// If error is nil, the aquire is successful and the revision must be
// stored as it is required to release the lock.
func (l pubLock) aquireLock() (uint64, error) {
	if lockStore == nil {
		return 0, nil // no cluster - lock is always granted
	}
	fmt.Printf("Lock store aquire\n")
	revision, err := lockStore.Create(context.Background(), string(l.name), []byte{1})
	fmt.Printf("Lock store aquired\n")
	if err != nil {
		return 0, err
	}
	return revision, nil
}
func (l pubLock) refreshLock(rev uint64) (uint64, error) {
	if lockStore == nil {
		return rev, nil
	}
	rev, err := lockStore.Update(context.Background(), string(l.name), []byte{1}, rev)
	return rev, err
}
func (l pubLock) releaseLock(revision uint64) {
	if lockStore == nil {
		return
	}
	lockStore.Delete(context.Background(), string(l.name), jetstream.LastRevision(revision))
}

// Lock is an exported distributed lock for use by process blocks.
// It ensures at-most-once side effects in clustered deployments.
type Lock struct {
	key SubjectName
}

// NewLock creates a Lock for the given key. The key must be unique per side-effect
// (e.g. "alarm.org.device.tag.hi" for a high-limit alarm on a specific tag).
func NewLock(key SubjectName) *Lock {
	return &Lock{key: key}
}

// TryAcquire attempts to acquire the lock. Returns (true, revision) on success.
// Returns (false, 0) if the lock is currently held by another server.
func (l *Lock) TryAcquire() (bool, uint64) {
	inner := NewPubLock(l.key)
	rev, err := inner.aquireLock()
	if err != nil {
		return false, 0
	}
	return true, rev
}

// Release releases the lock using the revision returned by TryAcquire.
func (l *Lock) Release(rev uint64) {
	inner := NewPubLock(l.key)
	inner.releaseLock(rev)
}
*/
