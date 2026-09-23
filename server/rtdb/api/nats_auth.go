package api

import (
	"context"
	"os"
	"time"

	"github.com/xact-iot/xact/rtdb/nats"
)

// AuthenticateNATS rechecks live account/session state on every connection,
// including reconnects, and derives broker permissions from the HTTP roles.
func (s *Server) AuthenticateNATS(ctx context.Context, bearer string) (nats.BrowserSession, bool) {
	claims, ok := authenticateBearer(ctx, s.jwtSecret, s.db, bearer)
	if !ok {
		return nats.BrowserSession{}, false
	}
	expires := time.Now().Add(24 * time.Hour)
	if claims.ExpiresAt != nil {
		expires = claims.ExpiresAt.Time
	} else if claims.TokenType != "agent" {
		return nats.BrowserSession{}, false
	}
	ctx = context.WithValue(ctx, claimsContextKey, claims)
	return nats.BrowserSession{
		Org:          claims.TenantID,
		UserID:       claims.UserID,
		ExpiresAt:    expires,
		ReadTree:     s.checkUIPermission(ctx, "nodes", "read"),
		ReadTags:     s.checkUIPermission(ctx, "tags", "read"),
		SendCommands: parseAPIEnvBool(os.Getenv("NATS_BROWSER_ALLOW_COMMANDS"), false) && s.checkUIPermission(ctx, "tags", "write"),
	}, true
}
