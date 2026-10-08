package nats

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xact-iot/xact/rtdb/tree"
)

// DecodeTagValueChanges returns absolute tag paths for live consumers. A batch
// contains replay values as well, but only Changed should fire live triggers.
func DecodeTagValueChanges(subject string, data []byte) (map[string]tree.TagValue, error) {
	batchPrefix := BroadcastStreamPrefix + string(TagBatchStream) + "."
	if strings.HasPrefix(subject, batchPrefix) {
		group := strings.TrimPrefix(subject, batchPrefix)
		index := strings.LastIndexByte(group, '.')
		if index <= 0 {
			return nil, fmt.Errorf("invalid batch subject")
		}
		group = group[:index]
		var batch TagValueBatch
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
		changes := make(map[string]tree.TagValue, len(batch.Changed))
		for _, path := range batch.Changed {
			value, ok := batch.Values[path]
			if !ok || !strings.HasPrefix(path, group+".") || strings.Contains(path[len(group)+1:], ".") {
				return nil, fmt.Errorf("tag does not belong to batch group")
			}
			if value.Type == "value" {
				changes[path] = value
			}
		}
		return changes, nil
	}
	legacyPrefix := BroadcastStreamPrefix + string(TagValueStream) + "."
	if !strings.HasPrefix(subject, legacyPrefix) {
		return nil, fmt.Errorf("not a tag subject")
	}
	var values map[string]tree.TagValue
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	changes := map[string]tree.TagValue{}
	for _, value := range values {
		changes[strings.TrimPrefix(subject, legacyPrefix)] = value
	}
	return changes, nil
}
