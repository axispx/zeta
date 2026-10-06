package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// codexLoginTimeout bounds how long the loopback listener waits for the
// browser to come back before giving up.
const codexLoginTimeout = 5 * time.Minute

// codexBrowserFlow is an in-flight ChatGPT login: the browser authorizes
// against auth.openai.com and the redirect lands on a localhost listener.
type codexBrowserFlow struct {
	authorizeURL string
	redirectURI  string
	verifier     string
	state        string

	srv     *http.Server
	results chan callbackResult
	closed  chan struct{}
	once    sync.Once
}

// callbackResult is the single redirect outcome (code or error) Wait consumes.
type callbackResult struct {
	code string
	err  error
}

// codexBeginBrowser binds the loopback listener and returns the flow whose URL
// the caller opens in a browser.
func codexBeginBrowser() (Flow, error) {
	verifier, challenge, err := pkcePair()
	if err != nil {
		return nil, err
	}
	state, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	listeners, err := listenCodexLoopback()
	if err != nil {
		return nil, err
	}
	port := listeners[0].Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://%s:%d%s", CodexRedirectHost, port, CodexRedirectPath)

	f := &codexBrowserFlow{
		authorizeURL: codexAuthorizeURL(redirectURI, challenge, state),
		redirectURI:  redirectURI,
		verifier:     verifier,
		state:        state,
		results:      make(chan callbackResult, 1),
		closed:       make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(CodexRedirectPath, f.handleCallback)
	f.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	for _, ln := range listeners {
		go func(ln net.Listener) { _ = f.srv.Serve(ln) }(ln)
	}
	return f, nil
}

// listenCodexLoopback binds the registered redirect port on both loopback
// families: "localhost" resolves to 127.0.0.1 or ::1 depending on the machine,
// and the browser follows whichever it picks. The IPv6 listener is best-effort
// — it commonly fails on hosts with IPv6 disabled, where v4 alone suffices.
func listenCodexLoopback() ([]net.Listener, error) {
	primary, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", CodexRedirectPort))
	if err != nil {
		return nil, fmt.Errorf("codex login: cannot listen on port %d: %w", CodexRedirectPort, err)
	}
	listeners := []net.Listener{primary}
	port := primary.Addr().(*net.TCPAddr).Port
	if v6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", port)); err == nil {
		listeners = append(listeners, v6)
	}
	return listeners, nil
}

// codexAuthorizeURL builds the authorization URL. The two non-standard query
// params are what make auth.openai.com show the ChatGPT-plan consent screen
// instead of the platform API-key flow.
func codexAuthorizeURL(redirectURI, challenge, state string) string {
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {CodexClientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {CodexScope},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {CodexOriginator},
	}
	return CodexAuthorizeURL + "?" + q.Encode()
}

func (f *codexBrowserFlow) URL() string      { return f.authorizeURL }
func (f *codexBrowserFlow) UserCode() string { return "" }

func (f *codexBrowserFlow) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// A stale tab (or a second redirect) must not consume the live flow.
	if q.Get("state") != f.state {
		http.Error(w, "State mismatch. Return to zeta and start the sign-in again.", http.StatusBadRequest)
		return
	}
	if code := q.Get("error"); code != "" {
		f.deliver(callbackResult{err: fmt.Errorf("codex login: %s %s", code, q.Get("error_description"))})
		http.Error(w, "Sign-in failed. Return to zeta and try again.", http.StatusBadRequest)
		return
	}
	code := q.Get("code")
	if code == "" {
		f.deliver(callbackResult{err: errors.New("codex login: callback carried no authorization code")})
		http.Error(w, "Missing authorization code. Return to zeta and try again.", http.StatusBadRequest)
		return
	}
	f.deliver(callbackResult{code: code})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, codexCallbackPage)
}

// deliver hands the first result to Wait and drops later ones (the browser can
// retry the redirect, and Wait consumes exactly one).
func (f *codexBrowserFlow) deliver(res callbackResult) {
	select {
	case f.results <- res:
	default:
	}
}

func (f *codexBrowserFlow) Wait(ctx context.Context) (*TokenResponse, error) {
	defer f.Close()
	timer := time.NewTimer(codexLoginTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.closed:
		return nil, errors.New("codex login cancelled")
	case <-timer.C:
		return nil, errors.New("codex login timed out")
	case res := <-f.results:
		if res.err != nil {
			return nil, res.err
		}
		return codexExchange(ctx, res.code, f.verifier, f.redirectURI)
	}
}

func (f *codexBrowserFlow) Close() {
	f.once.Do(func() {
		close(f.closed)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = f.srv.Shutdown(ctx)
	})
}

// codexExchange redeems the authorization code. The ChatGPT account id is
// required from here on: without it every backend request would be rejected.
func codexExchange(ctx context.Context, code, verifier, redirectURI string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {CodexClientID},
		"code_verifier": {verifier},
	}
	tok, err := tokenRequest(ctx, CodexTokenURL, form)
	if err != nil {
		return nil, err
	}
	tok = codexDecorate(tok)
	if tok.AccountID == "" {
		return nil, errors.New("codex login: token response has no ChatGPT account id")
	}
	return tok, nil
}

// codexRefresh renews an access token. The account id rides along when the
// provider echoes an id_token; callers keep the stored one otherwise.
func codexRefresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {CodexClientID},
		"refresh_token": {refreshToken},
	}
	tok, err := tokenRequest(ctx, CodexTokenURL, form)
	if err != nil {
		return nil, err
	}
	return codexDecorate(tok), nil
}

// codexDecorate adds the identity metadata the token endpoint does not return
// as a field: the ChatGPT account id and an expiry read off the access token
// when the response omits expires_in.
func codexDecorate(tok *TokenResponse) *TokenResponse {
	if tok == nil {
		return tok
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = jwtExpiresIn(tok.AccessToken)
	}
	if tok.IDToken != "" {
		tok.AccountID = jwtAccountID(tok.IDToken)
	}
	if tok.AccountID == "" {
		tok.AccountID = jwtAccountID(tok.AccessToken)
	}
	return tok
}

// jwtAccountID reads chatgpt_account_id out of a token's claims. OpenAI nests
// it under the https://api.openai.com/auth claim, and newer tokens also carry
// it at the top level.
func jwtAccountID(token string) string {
	claims := jwtClaims(token)
	if v, ok := claims["chatgpt_account_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	auth, ok := claims["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := auth["chatgpt_account_id"].(string)
	return strings.TrimSpace(v)
}

// jwtExpiresIn is the access token's remaining lifetime in seconds, or 0 when
// the token is not a readable JWT.
func jwtExpiresIn(token string) int64 {
	v, ok := jwtClaims(token)["exp"].(float64)
	if !ok {
		return 0
	}
	d := time.Until(time.Unix(int64(v), 0))
	if d <= 0 {
		return 0
	}
	return int64(d.Seconds())
}

// jwtClaims decodes a JWT payload without verifying its signature. The token
// arrives over TLS from the issuer's token endpoint; zeta reads only its
// account id and expiry, and never treats the claims as authorization.
func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

// pkcePair generates an RFC 7636 verifier and its S256 challenge.
func pkcePair() (verifier, challenge string, err error) {
	verifier, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// codexCallbackPage is shown in the browser once the redirect lands. It only
// has to tell the user they can close the tab.
const codexCallbackPage = `<!doctype html>
<meta charset="utf-8">
<title>Signed in to Codex</title>
<body style="font-family: system-ui, sans-serif; margin: 4rem auto; max-width: 30rem">
<h1 style="font-size: 1.25rem">Signed in</h1>
<p>You can close this tab and return to zeta.</p>
</body>
`
