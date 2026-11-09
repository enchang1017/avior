package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/token"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// TokenService is the part of FEAT-053 this layer needs.
type TokenService interface {
	Mint(ctx context.Context, caller identity.Caller, name string, lifetimeDays int) (token.Minted, error)
	Resolve(ctx context.Context, value string) (identity.Caller, error)
	List(ctx context.Context, caller identity.Caller) ([]token.Token, error)
	Revoke(ctx context.Context, caller identity.Caller, id uuid.UUID) error
	// MintForService and RevokeForService are FEAT-063: an administrator
	// acting on a service account's tokens rather than their own.
	MintForService(ctx context.Context, subject uuid.UUID, name string, lifetimeDays int) (token.Minted, error)
	RevokeForService(ctx context.Context, subject, id uuid.UUID) error
}

// viaTokenKey marks a request whose caller arrived on an API token rather
// than a session. FEAT-053 AC-6: the endpoints that manage tokens and accounts
// read it and refuse, because a token that can mint a token has no expiry.
type viaTokenKey struct{}

// ViaToken reports whether this request's caller was resolved from an API
// token.
func ViaToken(ctx context.Context) bool {
	via, ok := ctx.Value(viaTokenKey{}).(bool)
	return ok && via
}

// AuthenticateToken resolves an API token into the caller it acts as.
//
// It runs after the session middleware and only where that one attached
// nobody: a cookie wins, so a browser that also happens to send a header is
// still the person signed in. It runs before the release-token middleware for
// the opposite reason — the two tables are disjoint, an API token that does not
// resolve here falls through to be tried there, and either middleware leaving
// the context empty means the handler answers 401.
//
// The caller it attaches is a full identity.Caller, unlike the release issuer,
// because a token acts as its owner: the audit row names them, the grants are
// theirs, and the permission check reads what the token holds — which is the
// account's minus every decision, drawn by the service.
func AuthenticateToken(tokens TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, already := CallerFrom(r.Context()); already || PublicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			value := bearerToken(r)
			if value == "" {
				next.ServeHTTP(w, r)
				return
			}
			caller, err := tokens.Resolve(r.Context(), value)
			if err != nil {
				// A token that does not resolve is not a fault to report. The
				// handler answers 401 on the empty context, and a release token
				// still gets its turn.
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), callerKey, caller)
			ctx = context.WithValue(ctx, viaTokenKey{}, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CreateToken mints a token for the caller. FEAT-053 AC-1.
func (s *Server) CreateToken(
	ctx context.Context, request gen.CreateTokenRequestObject,
) (gen.CreateTokenResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.CreateToken401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.tokens == nil {
		return nil, errors.New("api: token service is not wired")
	}
	lifetime := 0
	if request.Body.ExpiresInDays != nil {
		lifetime = *request.Body.ExpiresInDays
	}
	minted, err := s.tokens.Mint(ctx, caller, request.Body.Name, lifetime)
	switch {
	case errors.Is(err, token.ErrNameBlank):
		return gen.CreateToken422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "token.name_blank", Message: "Give the token a name, so you can tell it apart from the others later.",
		}}, nil
	case errors.Is(err, token.ErrLifetimeOutOfRange):
		return gen.CreateToken422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "token.lifetime_out_of_range", Message: "A token lasts between one and 730 days (two years).",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.CreateToken201JSONResponse{
		Id: minted.ID, Name: minted.Name, CreatedAt: minted.CreatedAt, ExpiresAt: minted.ExpiresAt,
		LastUsedAt: nullable.NewNullNullable[time.Time](),
		RevokedAt:  nullable.NewNullNullable[time.Time](),
		Token:      minted.Value,
	}, nil
}

// ListTokens returns the caller's own tokens. FEAT-053 AC-8.
func (s *Server) ListTokens(
	ctx context.Context, _ gen.ListTokensRequestObject,
) (gen.ListTokensResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListTokens401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.tokens == nil {
		return nil, errors.New("api: token service is not wired")
	}
	tokens, err := s.tokens.List(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListTokens200JSONResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, apiToken(t))
	}
	return out, nil
}

// RevokeToken revokes one of the caller's tokens. FEAT-053 AC-9.
func (s *Server) RevokeToken(
	ctx context.Context, request gen.RevokeTokenRequestObject,
) (gen.RevokeTokenResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.RevokeToken401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.tokens == nil {
		return nil, errors.New("api: token service is not wired")
	}
	err := s.tokens.Revoke(ctx, caller, request.TokenId)
	switch {
	case errors.Is(err, token.ErrNotFound):
		return gen.RevokeToken404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: "token.not_found", Message: "No token of yours has that id.",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.RevokeToken204Response{}, nil
}

func apiToken(t token.Token) gen.ApiToken {
	out := gen.ApiToken{
		Id: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt,
		LastUsedAt: nullable.NewNullNullable[time.Time](),
		RevokedAt:  nullable.NewNullNullable[time.Time](),
	}
	if t.LastUsedAt != nil {
		out.LastUsedAt = nullable.NewNullableWithValue(*t.LastUsedAt)
	}
	if t.RevokedAt != nil {
		out.RevokedAt = nullable.NewNullableWithValue(*t.RevokedAt)
	}
	return out
}
