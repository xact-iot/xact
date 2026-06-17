package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xact-iot/xact/sqldb"
)

// contextKey is used for context values
type contextKey string

const claimsContextKey contextKey = "jwt_claims"

// JWTClaims represents the JWT claims structure
type JWTClaims struct {
	UserID       string   `json:"user_id"`
	Username     string   `json:"username"`
	TenantID     string   `json:"tenant_id"`
	Roles        []string `json:"roles"`
	AllowedOrgs  []string `json:"allowed_orgs"`
	TokenVersion int      `json:"token_version"`
	TokenType    string   `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

type agentTokenResolver interface {
	ResolveAgentToken(ctx context.Context, raw string) (*sqldb.AgentToken, error)
	TouchAgentToken(ctx context.Context, id int) error
}

// JWTAuth middleware validates JWT tokens
func JWTAuth(secret []byte, dbs ...sqldb.DB) func(http.Handler) http.Handler {
	var db sqldb.DB
	if len(dbs) > 0 {
		db = dbs[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				unauthorized(w)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				unauthorized(w)
				return
			}

			tokenString := parts[1]

			parser := jwt.NewParser(
				jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			)

			token, err := parser.ParseWithClaims(
				tokenString,
				&JWTClaims{},
				func(token *jwt.Token) (interface{}, error) {
					return secret, nil
				},
			)

			if err != nil || !token.Valid {
				if claims, ok := resolveAgentBearer(r.Context(), db, tokenString); ok {
					ctx := context.WithValue(r.Context(), claimsContextKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				unauthorized(w)
				return
			}

			claims, ok := token.Claims.(*JWTClaims)
			if !ok {
				unauthorized(w)
				return
			}
			if db != nil {
				if !validateLiveAuthState(r.Context(), db, claims) {
					unauthorized(w)
					return
				}
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolveAgentBearer(ctx context.Context, db sqldb.DB, raw string) (*JWTClaims, bool) {
	resolver, ok := db.(agentTokenResolver)
	if !ok {
		return nil, false
	}
	token, err := resolver.ResolveAgentToken(ctx, raw)
	if err != nil || token == nil || token.OrgName == "" {
		return nil, false
	}
	_ = resolver.TouchAgentToken(ctx, token.ID)
	return &JWTClaims{
		UserID:       "0",
		Username:     "agent:" + token.Name,
		TenantID:     token.OrgName,
		Roles:        token.Roles,
		AllowedOrgs:  []string{token.OrgName},
		TokenVersion: 0,
		TokenType:    "agent",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  token.Name,
			Issuer:   "xact",
			IssuedAt: jwt.NewNumericDate(token.CreatedAt),
		},
	}, true
}

func validateLiveAuthState(ctx context.Context, db sqldb.DB, claims *JWTClaims) bool {
	if claims.TokenType == "agent" {
		return true
	}
	userID, err := strconv.Atoi(claims.UserID)
	if err != nil || userID <= 0 {
		return false
	}
	active, tokenVersion, err := db.GetUserAuthState(ctx, userID)
	if err != nil || !active {
		return false
	}
	return claims.TokenVersion > 0 && claims.TokenVersion == tokenVersion
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "Unauthorized",
	})
}

// // JWTAuth middleware validates JWT tokens
// func JWTAuth(secret []byte) func(http.Handler) http.Handler {
// 	return func(next http.Handler) http.Handler {
// 		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 			// Get Authorization header
// 			authHeader := r.Header.Get("Authorization")
// 			if authHeader == "" {
// 				w.WriteHeader(http.StatusUnauthorized)
// 				respondWithError(w, "Missing authorization header")
// 				return
// 			}

// 			// Extract Bearer token
// 			parts := strings.SplitN(authHeader, " ", 2)
// 			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
// 				w.WriteHeader(http.StatusUnauthorized)
// 				respondWithError(w, "Invalid authorization header format")
// 				return
// 			}

// 			tokenString := parts[1]

// 			// Parse and validate token
// 			token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
// 				// Validate signing method
// 				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
// 					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
// 				}
// 				return secret, nil
// 			})

// 			if err != nil {
// 				w.WriteHeader(http.StatusUnauthorized)
// 				respondWithError(w, "Invalid token: "+err.Error())
// 				return
// 			}

// 			if !token.Valid {
// 				w.WriteHeader(http.StatusUnauthorized)
// 				respondWithError(w, "Invalid token")
// 				return
// 			}

// 			// Add claims to context
// 			if claims, ok := token.Claims.(*JWTClaims); ok {
// 				r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
// 			}

// 			next.ServeHTTP(w, r)
// 		})
// 	}
// }

// GetClaimsFromContext retrieves JWT claims from context
func GetClaimsFromContext(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*JWTClaims)
	return claims, ok
}

// OrgSandbox requires an authenticated organisation context for RTDB routes.
// The handlers normalise request paths into that organisation. Empty/root paths
// pass through so handlers can return the org root.
func OrgSandbox(_ string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok || claims.TenantID == "" {
				w.WriteHeader(http.StatusForbidden)
				respondWithError(w, "no organisation context in token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// respondWithError sends an error response
func respondWithError(w http.ResponseWriter, message string) {
	type errorResponse struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	w.Header().Set("Content-Type", "application/json")
	response := errorResponse{
		Error:   "Unauthorized",
		Message: message,
	}

	json.NewEncoder(w).Encode(response)
}
