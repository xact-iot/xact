package visualscripts

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// PathMatcher is the shared contract used by exact and wildcard tag triggers.
// Wildcards are segment-oriented: neither * nor ? can cross a dot separator.
type PathMatcher interface {
	Match(path string) bool
	Pattern() string
	Exact() bool
}

type pathMatcher struct {
	pattern  string
	segments []string
	exact    bool
}

func NormalizePathPattern(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool { return r == '.' || r == '/' })
	return strings.Join(parts, ".")
}

func CompilePathPattern(value string) (PathMatcher, error) {
	pattern := NormalizePathPattern(value)
	if pattern == "" {
		return nil, errors.New("path pattern is required")
	}
	segments := strings.Split(pattern, ".")
	for _, segment := range segments {
		if segment == "" {
			return nil, errors.New("path pattern contains an empty segment")
		}
	}
	return &pathMatcher{pattern: pattern, segments: segments, exact: !strings.ContainsAny(pattern, "*?")}, nil
}

func (m *pathMatcher) Pattern() string { return m.pattern }
func (m *pathMatcher) Exact() bool     { return m.exact }
func (m *pathMatcher) Match(value string) bool {
	path := NormalizePathPattern(value)
	if m.exact {
		return path == m.pattern
	}
	segments := strings.Split(path, ".")
	if len(segments) != len(m.segments) {
		return false
	}
	for i := range segments {
		if !matchSegment(m.segments[i], segments[i]) {
			return false
		}
	}
	return true
}

func matchSegment(pattern, value string) bool {
	// Dynamic programming avoids regex injection and pathological backtracking.
	valueRunes := []rune(value)
	previous := make([]bool, len(valueRunes)+1)
	previous[0] = true
	for _, token := range pattern {
		current := make([]bool, len(valueRunes)+1)
		switch token {
		case '*':
			current[0] = previous[0]
			for i := 1; i <= len(valueRunes); i++ {
				current[i] = previous[i] || current[i-1]
			}
		case '?':
			for i := 1; i <= len(valueRunes); i++ {
				current[i] = previous[i-1]
			}
		default:
			for i := 1; i <= len(valueRunes); i++ {
				current[i] = previous[i-1] && valueRunes[i-1] == token
			}
		}
		previous = current
	}
	return previous[len(valueRunes)]
}

type TagChange struct {
	OrgName   string
	TagPath   string
	Value     any
	Timestamp time.Time
}

type TagChangeCallback func(TagChange)

type tagRegistration struct {
	id       string
	matcher  PathMatcher
	callback TagChangeCallback
}

// TagChangeRouter owns compiled registrations and provides an exact-path fast
// path. NATS subscription ownership can be attached to Dispatch without changing
// trigger or compiler contracts in the RTDB delivery phase.
type TagChangeRouter struct {
	mu           sync.RWMutex
	exact        map[string]map[string]map[string]tagRegistration
	wildcard     map[string]map[string]tagRegistration
	maxWildcards int
	maxFanout    int
}

func NewTagChangeRouter(maxWildcards, maxFanout int) *TagChangeRouter {
	if maxWildcards <= 0 {
		maxWildcards = 100
	}
	if maxFanout <= 0 {
		maxFanout = 100
	}
	return &TagChangeRouter{exact: make(map[string]map[string]map[string]tagRegistration), wildcard: make(map[string]map[string]tagRegistration), maxWildcards: maxWildcards, maxFanout: maxFanout}
}

func (r *TagChangeRouter) Register(org, id, pattern string, callback TagChangeCallback) (func(), error) {
	matcher, err := CompilePathPattern(pattern)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(org) == "" || strings.TrimSpace(id) == "" || callback == nil {
		return nil, errors.New("organisation, registration ID, and callback are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	registration := tagRegistration{id: id, matcher: matcher, callback: callback}
	if matcher.Exact() {
		if r.exact[org] == nil {
			r.exact[org] = make(map[string]map[string]tagRegistration)
		}
		if r.exact[org][matcher.Pattern()] == nil {
			r.exact[org][matcher.Pattern()] = make(map[string]tagRegistration)
		}
		r.exact[org][matcher.Pattern()][id] = registration
	} else {
		if len(r.wildcard[org]) >= r.maxWildcards {
			return nil, errors.New("wildcard registration limit exceeded")
		}
		if r.wildcard[org] == nil {
			r.wildcard[org] = make(map[string]tagRegistration)
		}
		r.wildcard[org][id] = registration
	}
	return func() {
		r.mu.Lock()
		if paths := r.exact[org]; paths != nil {
			if registrations := paths[matcher.Pattern()]; registrations != nil {
				delete(registrations, id)
			}
		}
		delete(r.wildcard[org], id)
		r.mu.Unlock()
	}, nil
}

func (r *TagChangeRouter) Dispatch(change TagChange) int {
	change.TagPath = NormalizePathPattern(change.TagPath)
	r.mu.RLock()
	matches := make([]tagRegistration, 0)
	for _, registration := range r.exact[change.OrgName][change.TagPath] {
		matches = append(matches, registration)
		if len(matches) >= r.maxFanout {
			break
		}
	}
	if len(matches) < r.maxFanout {
		for _, registration := range r.wildcard[change.OrgName] {
			if registration.matcher.Match(change.TagPath) {
				matches = append(matches, registration)
				if len(matches) >= r.maxFanout {
					break
				}
			}
		}
	}
	r.mu.RUnlock()
	for _, registration := range matches {
		registration.callback(change)
	}
	return len(matches)
}
