package api

import (
	"context"
	"net/http"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

type contextKey int

const (
	callerKey contextKey = iota
	tokenKey
)

// CallerFrom returns the authenticated caller, if the request carried a valid
// session. Handlers must check the second return value: treating a missing
// caller as an anonymous one would run statements with no identity and no
// permission set.
func CallerFrom(ctx context.Context) (identity.Caller, bool) {
	caller, ok := ctx.Value(callerKey).(identity.Caller)
	return caller, ok
}

// TokenFrom returns the raw session token the request carried.
func TokenFrom(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(tokenKey).(string)
	return token, ok
}

// SessionResolver turns a session token into the caller it belongs to.
type SessionResolver interface {
	Resolve(ctx context.Context, token string) (identity.Caller, error)
}

// PublicPaths are the endpoints reachable without a session. Everything else
// requires one. The list is closed rather than open: a new endpoint is private
// until someone deliberately adds it here.
var PublicPaths = map[string]bool{
	"/api/v1/health":     true,
	"/api/v1/auth/login": true,
}

// Authenticate resolves the session cookie and attaches the caller to the
// request context. It does not reject unauthenticated requests: handlers decide,
// so that an endpoint can serve both cases if it ever needs to.
func Authenticate(resolver SessionResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if PublicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			caller, err := resolver.Resolve(r.Context(), cookie.Value)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), callerKey, caller)
			ctx = context.WithValue(ctx, tokenKey, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
