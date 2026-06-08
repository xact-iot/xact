package tree

import "time"

// TagStatus string. Each letter when present represents a status. More than one can be present.
// Priority order: high to low (U, S, A, D). An empty string means Normal.
const (
	StatusUndefined = "U"
	StatusStale     = "S"
	StatusAlarm     = "A"
	StatusDeviation = "D"
)

// TagRuntime holds non-persisted runtime state for a tag
type TagRuntime struct {
	Status            string    `json:"-"`
	UpdatedTime       time.Time `json:"-"`
	ProcessBlocksData []any     `json:"-"`
	lastPubValue      any       `json:"-"`
	lastPubStatus     string    `json:"-"`
	lastPubUpdateTime time.Time `json:"-"`
}
