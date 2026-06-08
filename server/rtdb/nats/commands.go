package nats

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const CommandStreamName = "commands"
const CommandSubjectPrefix = "xact.command."

func PrepareCommandStream(nc *nats.Conn) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("command stream: %w", err)
	}
	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:              CommandStreamName,
		Subjects:          []string{CommandSubjectPrefix + ">"},
		Retention:         jetstream.WorkQueuePolicy,
		MaxAge:            24 * time.Hour,
		Storage:           jetstream.FileStorage,
		MaxMsgsPerSubject: 1000,
	})
	if err != nil {
		return fmt.Errorf("command stream: create: %w", err)
	}
	return nil
}
