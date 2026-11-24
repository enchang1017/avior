package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/auth"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// GoogleStateCookieName carries the signed per-login state, nonce, and PKCE
// verifier across the Google OIDC redirect round-trip.
const GoogleStateCookieName = "avior_oauth_state"

const googleStateCookieTTL = 10 * time.Minute

// GetAuthMethods reports which interactive sign-in flows this deployment
// accepts, so the sign-in view can offer Google Workspace when configured.
func (s *Server) GetAuthMethods(
	_ context.Context, _ gen.GetAuthMethodsRequestObject,
) (gen.GetAuthMethodsResponseObject, error) {
	return gen.GetAuthMethods200JSONResponse{
		Mode:     gen.AuthMethodsMode(s.auth.Mode()),
		Password: true,
		Google:   s.auth.GoogleConfigured(),
	}, nil
}

// StartGoogleAuth initiates a Google Workspace OIDC authorization code flow.
func (s *Server) StartGoogleAuth(
	ctx context.Context, request gen.StartGoogleAuthRequestObject,
) (gen.StartGoogleAuthResponseObject, error) {
	authURL, stateCookie, err := s.auth.StartGoogleAuth(ctx, deref(request.Params.Redirect))
	if errors.Is(err, auth.ErrGoogleNotConfigured) {
		return gen.StartGoogleAuth404JSONResponse{NotFoundJSONResponse: googleNotConfigured()}, nil
	}
	if err != nil {
		return nil, err
	}
	return googleStartResponse{
		location:    authURL,
		stateCookie: stateCookie,
		secure:      s.defaults.SecureCookies,
	}, nil
}

// GoogleAuthCallback validates Google's OIDC callback, opens an Avior session
// on success, and redirects the browser back into the console or to /login on
// refusal.
func (s *Server) GoogleAuthCallback(
	ctx context.Context, request gen.GoogleAuthCallbackRequestObject,
) (gen.GoogleAuthCallbackResponseObject, error) {
	if !s.auth.GoogleConfigured() {
		return gen.GoogleAuthCallback404JSONResponse{NotFoundJSONResponse: googleNotConfigured()}, nil
	}
	if errCode := strings.TrimSpace(deref(request.Params.Error)); errCode != "" {
		return googleCallbackFailureResponse{
			location: loginErrorRedirect("google_cancelled"),
			secure:   s.defaults.SecureCookies,
		}, nil
	}

	session, redirect, err := s.auth.CompleteGoogleAuth(
		ctx,
		deref(request.Params.AviorOauthState),
		deref(request.Params.State),
		deref(request.Params.Code),
	)
	if err != nil {
		// Every rejected Google sign-in answers with the same refusal code so
		// callers cannot tell a disabled account apart from a disallowed domain
		// or a declared local account.
		//nolint:nilerr // a refused sign-in is a redirect response, not a 500
		return googleCallbackFailureResponse{
			location: loginErrorRedirect("google_failed"),
			secure:   s.defaults.SecureCookies,
		}, nil
	}
	return googleCallbackSuccessResponse{
		session:  session,
		location: redirect,
		secure:   s.defaults.SecureCookies,
	}, nil
}

func googleNotConfigured() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code:    "auth.google_not_configured",
		Message: "Google Workspace sign-in is not enabled on this deployment.",
	}
}

func loginErrorRedirect(code string) string {
	return "/login?error=" + url.QueryEscape(code)
}

type googleStartResponse struct {
	location    string
	stateCookie string
	secure      bool
}

func (r googleStartResponse) VisitStartGoogleAuthResponse(w http.ResponseWriter) error {
	// SameSite=Lax is required so the browser includes the state cookie on the
	// top-level GET redirect returning from accounts.google.com.
	http.SetCookie(w, &http.Cookie{
		Name:     GoogleStateCookieName,
		Value:    r.stateCookie,
		Path:     "/api/v1/auth/google/callback",
		MaxAge:   int(googleStateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.secure,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", r.location)
	w.WriteHeader(http.StatusFound)
	return nil
}

type googleCallbackSuccessResponse struct {
	session  *identity.Session
	location string
	secure   bool
}

func (r googleCallbackSuccessResponse) VisitGoogleAuthCallbackResponse(w http.ResponseWriter) error {
	clearGoogleStateCookie(w, r.secure)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    r.session.Token,
		Path:     "/",
		Expires:  r.session.ExpiresAt,
		HttpOnly: true,
		Secure:   r.secure,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", r.location)
	w.WriteHeader(http.StatusFound)
	return nil
}

type googleCallbackFailureResponse struct {
	location string
	secure   bool
}

func (r googleCallbackFailureResponse) VisitGoogleAuthCallbackResponse(w http.ResponseWriter) error {
	clearGoogleStateCookie(w, r.secure)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", r.location)
	w.WriteHeader(http.StatusFound)
	return nil
}

func clearGoogleStateCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     GoogleStateCookieName,
		Value:    "",
		Path:     "/api/v1/auth/google/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
