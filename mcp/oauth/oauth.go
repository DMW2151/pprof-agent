// Package oauth implements the minimum OAuth 2.0 authorization server surface
// that Claude Code's HTTP MCP transport demands on connect.
//
// # Why this exists
//
// Claude Code (2.1.x) always runs an RFC 7591 Dynamic Client Registration and
// an OIDC-style discovery handshake against every HTTP MCP server, regardless
// of whether a static bearer token is pre-provisioned via `claude mcp add
// --header`. If those probes fail, the client flips its session to "not
// authenticated" and hides all tools — even though the parallel `tools/list`
// over the pre-provisioned bearer header succeeded. We therefore provide the
// endpoints Claude expects and have them culminate in delivering the same
// pre-configured bearer token that the middleware validates.
//
// # Flow
//
//  1. GET  /.well-known/oauth-authorization-server
//     GET  /.well-known/openid-configuration
//     GET  /mcp/.well-known/openid-configuration
//     → AS metadata (RFC 8414) advertising the endpoints below.
//  2. POST /register
//     → Accept any client metadata and return an opaque client_id.
//  3. GET  /authorize?client_id=...&redirect_uri=...&code_challenge=...&state=...
//     → Auto-approve (trusted local environment); 302 to redirect_uri with
//     `code` and `state`. Records the PKCE challenge for token-step check.
//  4. POST /token (grant_type=authorization_code)
//     → Verify code + PKCE, return access_token set to the shared secret.
//
// The access_token we issue is the exact value that the bearer middleware
// expects. That deliberate collision lets a single middleware path validate
// both operator-provisioned headers and OAuth-issued tokens.
//
// # Deploying remote
//
// This package is safe to expose publicly *only* if you consider the bearer
// token a shared secret. For a real multi-user deployment, replace the
// Issuer.authorize handler with a redirect to a real IdP (Auth0, Okta, etc.)
// and change Issuer.token to validate the IdP-issued code / exchange for a
// per-user token. The middleware, MCP server, and resource-metadata handlers
// stay the same.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Issuer is a minimal OAuth 2.0 authorization server that auto-approves every
// authorization request and mints access tokens equal to a configured secret.
type Issuer struct {
	// Token is the single bearer token this issuer hands out at /token.
	// /mcp's bearer middleware must accept the same value.
	Token string

	mu      sync.Mutex
	codes   map[string]codeEntry   // authorization codes awaiting exchange
	clients map[string]clientEntry // DCR-registered clients
}

type codeEntry struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	expiresAt           time.Time
}

type clientEntry struct {
	redirectURIs []string
	createdAt    time.Time
}

// New returns an Issuer that mints tokens equal to the provided shared secret.
func New(token string) *Issuer {
	return &Issuer{
		Token:   token,
		codes:   make(map[string]codeEntry),
		clients: make(map[string]clientEntry),
	}
}

// Register wires the issuer's endpoints onto mux. The handler set is fixed;
// callers should not register competing handlers for the same paths.
func (i *Issuer) Register(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", i.metadata)
	mux.HandleFunc("/.well-known/openid-configuration", i.metadata)
	// Claude Code probes this path-suffixed variant specifically.
	mux.HandleFunc("/mcp/.well-known/openid-configuration", i.metadata)
	mux.HandleFunc("/register", i.register)
	mux.HandleFunc("/authorize", i.authorize)
	mux.HandleFunc("/token", i.token)
}

// baseURL reconstructs the public origin from request headers so a single
// binary works whether it's reached directly on :8082 or behind a TLS proxy
// that terminates on a public hostname.
func baseURL(r *http.Request) string {
	scheme := "http"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (i *Issuer) metadata(w http.ResponseWriter, r *http.Request) {
	b := baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                b,
		"authorization_endpoint":                b + "/authorize",
		"token_endpoint":                        b + "/token",
		"registration_endpoint":                 b + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mcp"},
	})
}

// register implements RFC 7591 Dynamic Client Registration. We accept any
// metadata and mint an opaque client_id. No client_secret: we use PKCE as the
// proof-of-possession mechanism (token_endpoint_auth_method=none).
func (i *Issuer) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	var body struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	clientID := randomID(16)
	i.mu.Lock()
	i.clients[clientID] = clientEntry{redirectURIs: body.RedirectURIs, createdAt: time.Now()}
	i.mu.Unlock()

	slog.Info("oauth: client registered", "client_id", clientID, "client_name", body.ClientName, "redirect_uris", body.RedirectURIs)
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              body.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authorize auto-approves the authorization request and redirects back with a
// single-use code. For a multi-user deployment this is where you'd insert a
// real user-consent step or delegate to an upstream IdP.
func (i *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	clientID := q.Get("client_id")
	if redirectURI == "" || clientID == "" {
		http.Error(w, "missing client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}

	code := randomID(24)
	i.mu.Lock()
	i.codes[code] = codeEntry{
		clientID:            clientID,
		redirectURI:         redirectURI,
		codeChallenge:       q.Get("code_challenge"),
		codeChallengeMethod: q.Get("code_challenge_method"),
		expiresAt:           time.Now().Add(5 * time.Minute),
	}
	i.mu.Unlock()

	slog.Info("oauth: authorize auto-approved", "client_id", clientID, "redirect_uri", redirectURI)
	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// token exchanges an authorization code for the shared-secret access token.
// Verifies PKCE so an intercepted code can't be redeemed by another party.
func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	code := r.Form.Get("code")

	i.mu.Lock()
	entry, ok := i.codes[code]
	if ok {
		delete(i.codes, code)
	}
	i.mu.Unlock()

	if !ok || time.Now().After(entry.expiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if rdr := r.Form.Get("redirect_uri"); rdr != "" && rdr != entry.redirectURI {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "redirect_uri mismatch"})
		return
	}
	if err := verifyPKCE(r.Form.Get("code_verifier"), entry.codeChallenge, entry.codeChallengeMethod); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": err.Error()})
		return
	}

	slog.Info("oauth: token issued", "client_id", entry.clientID)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": i.Token,
		"token_type":   "Bearer",
		"expires_in":   3153600000, // 100 years — the token is a static shared secret
		"scope":        "mcp",
	})
}

// verifyPKCE validates the code_verifier against the stored challenge. When no
// challenge was registered (some clients skip PKCE) we accept the exchange —
// this is a local-trust issuer, not a general-purpose AS. Remote deployments
// should require PKCE by removing the empty-challenge branch.
func verifyPKCE(verifier, challenge, method string) error {
	if challenge == "" {
		return nil
	}
	if verifier == "" {
		return errors.New("missing code_verifier")
	}
	switch method {
	case "", "plain":
		if verifier != challenge {
			return errors.New("pkce mismatch")
		}
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		got := base64.RawURLEncoding.EncodeToString(sum[:])
		if got != challenge {
			return errors.New("pkce mismatch")
		}
	default:
		return errors.New("unsupported code_challenge_method")
	}
	return nil
}

func randomID(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// rand.Read on a healthy Unix never fails; panic surfaces any
		// catastrophic entropy failure immediately rather than issuing
		// a predictable fallback.
		panic(err)
	}
	return hex.EncodeToString(b)
}
