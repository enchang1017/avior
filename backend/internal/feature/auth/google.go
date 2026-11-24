package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/platform/oidc"
)

// ErrGoogleNotConfigured reports a Google Workspace endpoint called when
// AVIOR_AUTH_MODE is not google.
var ErrGoogleNotConfigured = errors.New("auth: google workspace authentication is not configured")

const (
	googleFlowTTL    = 10 * time.Minute
	googleCacheTTL   = time.Hour
	maxOIDCBodyBytes = 1 << 20
)

// GoogleOIDCProvider is the third-party boundary for Google Workspace OpenID
// Connect. It holds discovery, authorization-code exchange, and JWKS retrieval
// behind a port so integration tests can drive either a local fake OIDC server
// or a direct stub (ADR-0002 R4).
type GoogleOIDCProvider interface {
	AuthorizationURL(ctx context.Context, state, nonce, codeChallenge, domainHint string) (string, error)
	ExchangeAndVerify(ctx context.Context, code, codeVerifier, nonce string, now time.Time) (oidc.VerifiedIdentity, error)
}

// GoogleConfig holds the runtime parameters for [HTTPGoogleProvider].
type GoogleConfig struct {
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	AllowedDomains []string
	Timeout        time.Duration
	HTTPClient     *http.Client
	Logger         *slog.Logger
}

// GoogleAuthenticator orchestrates Google Workspace OIDC login against Avior's
// account store, keeping ADR 0006's split: Google vouches for identity, Avior
// owns accounts, roles, and enablement.
type GoogleAuthenticator struct {
	provider       GoogleOIDCProvider
	repo           Repository
	stateSecret    []byte
	defaultRoles   []string
	allowedDomains []string
	logger         *slog.Logger
}

// NewGoogleAuthenticator validates defaultRoles and returns a Google Workspace
// authenticator. Refusing invalid or administrator default roles at startup
// matches FEAT-007 AC-13.
func NewGoogleAuthenticator(
	provider GoogleOIDCProvider,
	repo Repository,
	stateSecret []byte,
	allowedDomains []string,
	defaultRoles []string,
	logger *slog.Logger,
) (*GoogleAuthenticator, error) {
	if provider == nil {
		return nil, errors.New("auth: google oidc provider is required")
	}
	if repo == nil {
		return nil, errors.New("auth: repository is required")
	}
	if len(stateSecret) == 0 {
		return nil, errors.New("auth: state secret is required")
	}
	if len(allowedDomains) == 0 {
		return nil, errors.New("auth: at least one allowed google workspace domain is required")
	}
	for _, defaultRole := range defaultRoles {
		if !identity.Assignable(identity.Role(defaultRole)) {
			return nil, fmt.Errorf(
				"auth: AVIOR_GOOGLE_DEFAULT_ROLE names %q, which is not a role this build grants",
				defaultRole)
		}
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &GoogleAuthenticator{
		provider:       provider,
		repo:           repo,
		stateSecret:    stateSecret,
		defaultRoles:   defaultRoles,
		allowedDomains: allowedDomains,
		logger:         logger,
	}, nil
}

// Start creates a per-login state, nonce, and PKCE verifier, seals them for the
// browser cookie, and returns Google's authorization URL.
func (g *GoogleAuthenticator) Start(ctx context.Context, redirect string, now time.Time) (string, string, error) {
	state, err := oidc.RandomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate oauth state: %w", err)
	}
	nonce, err := oidc.RandomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate oidc nonce: %w", err)
	}
	codeVerifier, err := oidc.RandomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate pkce verifier: %w", err)
	}

	sealed, err := oidc.SealState(g.stateSecret, oidc.FlowState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		Redirect:     oidc.SanitizeRedirect(redirect),
		ExpiresAt:    now.Add(googleFlowTTL).Unix(),
	})
	if err != nil {
		return "", "", fmt.Errorf("seal oauth state: %w", err)
	}

	domainHint := ""
	if len(g.allowedDomains) == 1 {
		domainHint = g.allowedDomains[0]
	}
	authURL, err := g.provider.AuthorizationURL(
		ctx, state, nonce, oidc.CodeChallengeS256(codeVerifier), domainHint,
	)
	if err != nil {
		return "", "", fmt.Errorf("build google authorization url: %w", err)
	}
	return authURL, sealed, nil
}

// VerifyCallback validates the OAuth callback, exchanges the authorization code,
// verifies the Google Workspace ID token, and resolves or provisions the Avior
// account.
func (g *GoogleAuthenticator) VerifyCallback(
	ctx context.Context, stateCookie, queryState, code string, now time.Time,
) (Account, string, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(queryState) == "" {
		return Account{}, "", ErrInvalidCredentials
	}
	flow, err := oidc.OpenState(g.stateSecret, stateCookie, now)
	if err != nil {
		g.logger.WarnContext(ctx, "google callback rejected: invalid state cookie", "error", err.Error())
		return Account{}, "", ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare([]byte(queryState), []byte(flow.State)) != 1 {
		g.logger.WarnContext(ctx, "google callback rejected: state mismatch")
		return Account{}, "", ErrInvalidCredentials
	}

	ident, err := g.provider.ExchangeAndVerify(ctx, code, flow.CodeVerifier, flow.Nonce, now)
	if err != nil {
		g.logger.WarnContext(ctx, "google identity verification failed", "error", err.Error())
		return Account{}, "", ErrInvalidCredentials
	}

	account, err := g.resolveAccount(ctx, ident.Email)
	if err != nil {
		return Account{}, "", err
	}
	return account, flow.Redirect, nil
}

func (g *GoogleAuthenticator) resolveAccount(ctx context.Context, email string) (Account, error) {
	username := strings.ToLower(strings.TrimSpace(email))
	if username == "" || identity.IsServiceAccountName(username) {
		return Account{}, ErrInvalidCredentials
	}

	account, err := g.repo.FindAccount(ctx, username)
	if errors.Is(err, ErrAccountNotFound) {
		account, err = g.repo.ProvisionAccount(ctx, username, g.defaultRoles)
	}
	if err != nil {
		g.logger.WarnContext(ctx, "google account lookup or provisioning failed", "error", err.Error())
		return Account{}, ErrInvalidCredentials
	}
	// FEAT-056: a declared account authenticates locally and cannot be claimed by
	// an external identity provider. FEAT-063: service accounts never sign in.
	if account.Declared || account.Disabled || account.Service {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

type discoveryDocument struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type tokenResponse struct {
	IDToken string `json:"id_token"`
	Error   string `json:"error"`
}

// HTTPGoogleProvider speaks OpenID Connect Discovery, Authorization Code + PKCE
// token exchange, and JWKS retrieval over HTTP.
type HTTPGoogleProvider struct {
	config GoogleConfig
	client *http.Client

	mu             sync.RWMutex
	discovery      *discoveryDocument
	discoveryUntil time.Time
	jwks           oidc.JWKS
	jwksUntil      time.Time
}

// NewHTTPGoogleProvider validates cfg and returns a live Google OIDC provider.
func NewHTTPGoogleProvider(cfg GoogleConfig) (*HTTPGoogleProvider, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		return nil, errors.New("auth: AVIOR_GOOGLE_ISSUER_URL is required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("auth: AVIOR_GOOGLE_CLIENT_ID is required")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("auth: AVIOR_GOOGLE_CLIENT_SECRET is required")
	}
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, errors.New("auth: AVIOR_GOOGLE_REDIRECT_URL is required")
	}
	if len(cfg.AllowedDomains) == 0 {
		return nil, errors.New("auth: AVIOR_GOOGLE_ALLOWED_DOMAINS is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	cfg.IssuerURL = strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	return &HTTPGoogleProvider{config: cfg, client: httpClient}, nil
}

// AuthorizationURL implements [GoogleOIDCProvider].
func (p *HTTPGoogleProvider) AuthorizationURL(
	ctx context.Context, state, nonce, codeChallenge, domainHint string,
) (string, error) {
	doc, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(doc.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint: %w", err)
	}
	params, err := url.ParseQuery(endpoint.RawQuery)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint query: %w", err)
	}
	params.Set("response_type", "code")
	params.Set("client_id", p.config.ClientID)
	params.Set("redirect_uri", p.config.RedirectURL)
	params.Set("scope", "openid email profile")
	params.Set("state", state)
	params.Set("nonce", nonce)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("prompt", "select_account")
	if domainHint != "" {
		params.Set("hd", domainHint)
	}
	endpoint.RawQuery = params.Encode()
	return endpoint.String(), nil
}

// ExchangeAndVerify implements [GoogleOIDCProvider].
func (p *HTTPGoogleProvider) ExchangeAndVerify(
	ctx context.Context, code, codeVerifier, nonce string, now time.Time,
) (oidc.VerifiedIdentity, error) {
	doc, err := p.discover(ctx)
	if err != nil {
		return oidc.VerifiedIdentity{}, err
	}
	idToken, err := p.exchangeCode(ctx, doc.TokenEndpoint, code, codeVerifier)
	if err != nil {
		return oidc.VerifiedIdentity{}, err
	}
	opts := oidc.VerifyOptions{
		Issuer:         p.config.IssuerURL,
		ClientID:       p.config.ClientID,
		Nonce:          nonce,
		AllowedDomains: p.config.AllowedDomains,
		Now:            now,
	}
	keys, err := p.fetchJWKS(ctx, doc.JWKSURI, false)
	if err != nil {
		return oidc.VerifiedIdentity{}, err
	}
	ident, err := oidc.VerifyIDToken(idToken, keys, opts)
	if errors.Is(err, oidc.ErrUnknownKeyID) {
		// Refresh JWKS once when Google rotates signing keys.
		keys, err = p.fetchJWKS(ctx, doc.JWKSURI, true)
		if err != nil {
			return oidc.VerifiedIdentity{}, err
		}
		return oidc.VerifyIDToken(idToken, keys, opts)
	}
	return ident, err
}

func (p *HTTPGoogleProvider) discover(ctx context.Context) (*discoveryDocument, error) {
	p.mu.RLock()
	if p.discovery != nil && time.Now().Before(p.discoveryUntil) {
		doc := p.discovery
		p.mu.RUnlock()
		return doc, nil
	}
	p.mu.RUnlock()

	endpoint := p.config.IssuerURL + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	var doc discoveryDocument
	if err := p.doJSON(req, &doc); err != nil {
		return nil, fmt.Errorf("fetch oidc discovery: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return nil, errors.New("oidc discovery document missing required endpoints")
	}

	p.mu.Lock()
	p.discovery = &doc
	p.discoveryUntil = time.Now().Add(googleCacheTTL)
	p.mu.Unlock()
	return &doc, nil
}

func (p *HTTPGoogleProvider) exchangeCode(ctx context.Context, tokenEndpoint, code, codeVerifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"redirect_uri":  {p.config.RedirectURL},
		"code_verifier": {codeVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out tokenResponse
	if err := p.doJSON(req, &out); err != nil {
		return "", fmt.Errorf("exchange authorization code: %w", err)
	}
	if out.Error != "" || strings.TrimSpace(out.IDToken) == "" {
		return "", errors.New("token endpoint returned no id_token")
	}
	return out.IDToken, nil
}

func (p *HTTPGoogleProvider) fetchJWKS(ctx context.Context, jwksURI string, force bool) (oidc.JWKS, error) {
	if !force {
		p.mu.RLock()
		if len(p.jwks.Keys) > 0 && time.Now().Before(p.jwksUntil) {
			keys := p.jwks
			p.mu.RUnlock()
			return keys, nil
		}
		p.mu.RUnlock()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return oidc.JWKS{}, fmt.Errorf("build jwks request: %w", err)
	}
	var keys oidc.JWKS
	if err := p.doJSON(req, &keys); err != nil {
		return oidc.JWKS{}, fmt.Errorf("fetch jwks: %w", err)
	}
	if len(keys.Keys) == 0 {
		return oidc.JWKS{}, errors.New("jwks document contains no keys")
	}

	p.mu.Lock()
	p.jwks = keys
	p.jwksUntil = time.Now().Add(googleCacheTTL)
	p.mu.Unlock()
	return keys, nil
}

func (p *HTTPGoogleProvider) doJSON(req *http.Request, target any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCBodyBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
