package blocks

import "github.com/xact-iot/xact/events"

// eventsPublisher is the package-level publisher used by processing blocks that emit events.
// It is set once at startup via SetEventsPublisher.
var eventsPublisher *events.Publisher

// SetEventsPublisher configures the events publisher for all processing blocks.
// Must be called before any blocks start processing.
func SetEventsPublisher(publisher *events.Publisher) {
	eventsPublisher = publisher
}
