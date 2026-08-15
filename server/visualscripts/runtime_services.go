package visualscripts

import (
	"context"
	"time"
)

// RuntimeServices are the narrow, auditable integration points available to
// built-in action nodes. Keeping them as functions makes the graph runner
// independent of the RTDB, NATS, notification, and event implementations.
type RuntimeServices struct {
	TagRouter        *TagChangeRouter
	CanExecute       func() bool
	Now              func() time.Time
	SetTag           func(context.Context, Message, string, string, any) error
	SendControl      func(context.Context, Message, string, string, string, any, time.Duration) error
	SendNotification func(context.Context, Message, string, string, string, string, string) error
	LogEvent         func(context.Context, Message, string, string, string, string) error
}
