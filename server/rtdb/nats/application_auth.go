package nats

import (
	"crypto/subtle"
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/xact-iot/xact/applications"
	"os"
	"time"
)

// LoadApplicationServices grants only explicit administrator-installed subjects.
func (a *ClientAuthenticator) LoadApplicationServices(root string) error {
	all, e := applications.Load(root)
	if e != nil {
		return e
	}
	services := map[string]applications.Service{}
	for _, m := range all {
		for _, s := range m.Services {
			if os.Getenv(s.PasswordEnv) == "" {
				return fmt.Errorf("service %s requires environment variable %s", s.Username, s.PasswordEnv)
			}
			services[s.Username] = s
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.services = services
	return nil
}
func (a *ClientAuthenticator) checkApplication(client server.ClientAuthentication) bool {
	opts := client.GetOpts()
	a.mu.RLock()
	s, ok := a.services[opts.Username]
	a.mu.RUnlock()
	if !ok {
		return false
	}
	secret := os.Getenv(s.PasswordEnv)
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(opts.Password)) != 1 {
		return false
	}
	pub := &server.SubjectPermission{Deny: []string{">"}}
	sub := &server.SubjectPermission{Deny: []string{">"}}
	if len(s.Publish) > 0 {
		pub = &server.SubjectPermission{Allow: s.Publish}
	}
	if len(s.Subscribe) > 0 {
		sub = &server.SubjectPermission{Allow: s.Subscribe}
	}
	client.RegisterUser(&server.User{Username: s.Username, Permissions: &server.Permissions{Publish: pub, Subscribe: sub, Response: &server.ResponsePermission{MaxMsgs: 1, Expires: 30 * time.Second}}})
	return true
}
