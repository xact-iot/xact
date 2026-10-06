// Package applications loads administrator-installed, domain-independent extension manifests.
package applications

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var token = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Resource struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
}
type Service struct {
	Username    string   `json:"username"`
	PasswordEnv string   `json:"password_env"`
	Publish     []string `json:"publish"`
	Subscribe   []string `json:"subscribe"`
}
type Manifest struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Widgets    []string  `json:"widgets"`
	Resource   Resource  `json:"resource"`
	Operations []string  `json:"operations"`
	Services   []Service `json:"services,omitempty"`
}

func ValidToken(s string) bool { return token.MatchString(s) }
func Load(root string) ([]Manifest, error) {
	if root == "" {
		return nil, nil
	}
	files, e := filepath.Glob(filepath.Join(root, "applications", "*.json"))
	if e != nil {
		return nil, e
	}
	out := []Manifest{}
	seen := map[string]bool{}
	users := map[string]bool{}
	resources := map[string]bool{}
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			return nil, e
		}
		if len(b) > 128<<10 {
			return nil, fmt.Errorf("application manifest too large")
		}
		var m Manifest
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&m); e != nil {
			return nil, fmt.Errorf("manifest %s: %w", filepath.Base(f), e)
		}
		var trailing any
		if e = dec.Decode(&trailing); e != io.EOF {
			return nil, fmt.Errorf("manifest %s has trailing data", filepath.Base(f))
		}
		if !ValidToken(m.ID) || !ValidToken(m.Resource.ID) || seen[m.ID] || resources[m.Resource.ID] {
			return nil, fmt.Errorf("invalid or duplicate application/resource")
		}
		seen[m.ID] = true
		resources[m.Resource.ID] = true
		if len(m.Operations) == 0 || len(m.Resource.Permissions) == 0 {
			return nil, fmt.Errorf("application needs operations and permissions")
		}
		operations := map[string]bool{}
		for _, v := range m.Operations {
			if !ValidToken(v) || operations[v] {
				return nil, fmt.Errorf("invalid or duplicate operation")
			}
			operations[v] = true
		}
		for _, v := range m.Widgets {
			if !ValidToken(v) {
				return nil, fmt.Errorf("invalid widget")
			}
		}
		permissions := map[string]bool{}
		for _, v := range m.Resource.Permissions {
			if !ValidToken(v.Name) || permissions[v.Name] {
				return nil, fmt.Errorf("invalid or duplicate permission")
			}
			permissions[v.Name] = true
		}
		for _, s := range m.Services {
			if !strings.HasPrefix(s.Username, "app:") || !ValidToken(strings.TrimPrefix(s.Username, "app:")) || users[s.Username] || !ValidToken(s.PasswordEnv) {
				return nil, fmt.Errorf("invalid or duplicate service identity")
			}
			users[s.Username] = true
			if len(s.Publish) == 0 && len(s.Subscribe) == 0 {
				return nil, fmt.Errorf("empty service permissions")
			}
			for _, subject := range s.Publish {
				if !validServiceSubject(subject, m.ID, s.Username, true) {
					return nil, fmt.Errorf("manifest %s: service %q: invalid service publish subject %q", filepath.Base(f), s.Username, subject)
				}
			}
			for _, subject := range s.Subscribe {
				if !validServiceSubject(subject, m.ID, s.Username, false) {
					return nil, fmt.Errorf("manifest %s: service %q: invalid service subscribe subject %q", filepath.Base(f), s.Username, subject)
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}
func Subject(tenant, app, scope, op string) string {
	return "xact.app.v1." + tenant + "." + app + "." + scope + ".request." + op
}

// Service credentials stay inside their registered application namespace.
// The ingest and delete exceptions are tenant-specific and publish-only; reply inboxes are
// limited to the service identity and NATS response permissions cover handlers.
func validServiceSubject(subject, app, user string, publish bool) bool {
	if subject == "" || strings.ContainsAny(subject, " \t\r\n") {
		return false
	}
	parts := strings.Split(subject, ".")
	for i, p := range parts {
		if p == ">" && i != len(parts)-1 {
			return false
		}
		if p != "*" && p != ">" && !ValidToken(p) {
			return false
		}
	}
	if !publish && subject == "_INBOX."+strings.ReplaceAll(user, ":", "_")+".>" {
		return true
	}
	if len(parts) == 8 && parts[0] == "xact" && parts[1] == "app" && parts[2] == "v1" && ValidToken(parts[3]) && parts[4] == app && (ValidToken(parts[5]) || parts[5] == "*") && (parts[6] == "request" || parts[6] == "ingest") && (ValidToken(parts[7]) || parts[7] == "*") {
		return true
	}
	if publish && len(parts) >= 5 && parts[0] == "xact" && parts[1] == "internal" && (parts[2] == "ingest_request" || parts[2] == "delete_request") && ValidToken(parts[3]) {
		for _, p := range parts[4:] {
			if p == "*" {
				continue
			}
			if p == ">" {
				return true
			}
			if !ValidToken(p) {
				return false
			}
		}
		return true
	}
	return false
}
