package nats

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nats-io/nats-server/v2/server"
)

// BrowserSession is the live, authenticated HTTP session used by a NATS client.
// Even system administrators are scoped to the session's current organisation.
type BrowserSession struct {
	Org          string
	UserID       string
	ExpiresAt    time.Time
	ReadTree     bool
	ReadTags     bool
	SendCommands bool
}

type BrowserAuthenticator func(context.Context, string) (BrowserSession, bool)

// ClientAuthenticator keeps internal service access separate from browser
// sessions. Browser authentication fails closed until the API/database is ready.
type ClientAuthenticator struct {
	internalPassword string
	mu               sync.RWMutex
	browser          BrowserAuthenticator
}

func NewClientAuthenticator(internalPassword string) *ClientAuthenticator {
	return &ClientAuthenticator{internalPassword: internalPassword}
}

func (a *ClientAuthenticator) SetBrowserAuthenticator(auth BrowserAuthenticator) {
	a.mu.Lock()
	a.browser = auth
	a.mu.Unlock()
}

func (a *ClientAuthenticator) Check(client server.ClientAuthentication) bool {
	opts := client.GetOpts()
	if opts.Username == "internal" {
		if a.internalPassword == "" || subtle.ConstantTimeCompare([]byte(opts.Password), []byte(a.internalPassword)) != 1 {
			return false
		}
		client.RegisterUser(&server.User{Username: "internal", Permissions: &server.Permissions{
			Publish:   &server.SubjectPermission{Allow: []string{">"}},
			Subscribe: &server.SubjectPermission{Allow: []string{">"}},
		}})
		return true
	}
	if opts.Username != "browser" || opts.Password == "" {
		return false
	}
	a.mu.RLock()
	auth := a.browser
	a.mu.RUnlock()
	if auth == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, ok := auth(ctx, opts.Password)
	if !ok || !ValidBrowserSubjectToken(session.Org) || !ValidBrowserSubjectToken(session.UserID) || !session.ExpiresAt.After(time.Now()) {
		return false
	}
	permissions := &server.Permissions{
		Publish:   &server.SubjectPermission{Deny: []string{">"}},
		Subscribe: &server.SubjectPermission{Allow: []string{BrowserInboxPrefix(opts.Password) + ".>"}},
	}
	if session.ReadTree {
		permissions.Subscribe.Allow = append(permissions.Subscribe.Allow, "rtdb.tree."+session.Org, "rtdb.tree."+session.Org+".>")
	}
	if session.ReadTags {
		permissions.Subscribe.Allow = append(permissions.Subscribe.Allow, BroadcastStreamPrefix+"tagvalue."+session.Org+".>")
	}
	if session.UserID != "0" {
		permissions.Subscribe.Allow = append(permissions.Subscribe.Allow, BroadcastStreamPrefix+"mobile."+session.Org+"."+session.UserID)
	}
	if session.SendCommands {
		permissions.Publish = &server.SubjectPermission{Allow: []string{CommandSubjectPrefix + session.Org + ".>"}}
	}
	client.RegisterUser(&server.User{
		Username:           "browser:" + session.Org + ":" + session.UserID,
		Permissions:        permissions,
		ConnectionDeadline: session.ExpiresAt,
	})
	return true
}

// BrowserInboxPrefix isolates request replies between authenticated sessions.
func BrowserInboxPrefix(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "_INBOX." + hex.EncodeToString(sum[:16])
}

func ValidBrowserSubjectToken(value string) bool {
	return value != "" && !strings.ContainsAny(value, ".*>/\\") && strings.IndexFunc(value, unicode.IsSpace) < 0
}
