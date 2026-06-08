package scheduler

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NOTE:
// this can be changed to use the NATS natsServer.JetStreamIsLeader() instead of a KV lock

var schedLockStore jetstream.KeyValue

// PrepareSchedLocks creates (or re-opens) the NATS KV bucket used to
// de-duplicate scheduled task fires across cluster nodes.
// Call once at startup, after nats.PrepareLockStore().
func PrepareSchedLocks(nc *nats.Conn) error {
	clustered, _ := strconv.ParseBool(os.Getenv("CLUSTERED"))
	if !clustered {
		return nil
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	cfg := jetstream.KeyValueConfig{
		Bucket:  "sched-locks",
		TTL:     2 * 60 * 60 * 1_000_000_000, // 2h in nanoseconds
		Storage: jetstream.MemoryStorage,
	}

	schedLockStore, err = js.CreateOrUpdateKeyValue(context.Background(), cfg)
	if err != nil {
		log.Printf("scheduler: failed to create sched-locks KV bucket: %v", err)
	}
	return err
}

// TryAcquireForFire attempts to acquire the de-dup lock for a specific task
// fire. The lockKey should encode both the task ID and the cron fire time so
// that each minute-slot gets its own key.
//
// Returns true if this node won the lock (and should execute the task).
// Returns false if another node already holds the lock for this fire.
// In non-clustered deployments (schedLockStore == nil), always returns true.
func TryAcquireForFire(lockKey string) bool {
	if schedLockStore == nil {
		return true
	}
	_, err := schedLockStore.Create(context.Background(), lockKey, []byte{1})
	if err != nil {
		// ErrKeyExists means another node already grabbed this fire slot.
		if errors.Is(err, jetstream.ErrKeyExists) {
			return false
		}
		// Any other error: log and allow execution to avoid silently skipping tasks.
		log.Printf("scheduler: TryAcquireForFire(%q): %v - allowing execution", lockKey, err)
	}
	return true
}
