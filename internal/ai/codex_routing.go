package ai

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// The ChatGPT edge pins a client to a backend replica, and a replica is what
// holds that client's prompt cache. Three things carry the pin, and a client
// that sends none of them is re-balanced on every request: its history then
// hits only when a request happens to land where the previous one did.
//
//   - infrastructure cookies (__oailb, and Cloudflare's load-balancer cookies),
//     kept in routingCookies;
//   - the session-id header, which names the conversation (set from cacheKey);
//   - the x-codex-turn-state token, which the first response of a turn returns
//     and every later request of that turn must replay (turnState).

// turnStateHeader carries the sticky-routing token. The server issues it on a
// turn's first response; the client replays it unchanged for the rest of that
// turn and never across turns.
const turnStateHeader = "x-codex-turn-state"

// routingCookieNames are the only cookies kept. They are infrastructure
// cookies, not account or session credentials: the jar outlives any one
// client and must never hold anything user-specific.
var routingCookieNames = map[string]bool{
	"__oailb":         true,
	"__cf_bm":         true,
	"__cflb":          true,
	"__cfruid":        true,
	"__cfseq":         true,
	"__cfwaitingroom": true,
	"_cfuvid":         true,
}

// routingJar is a cookie jar that stores only routingCookieNames.
type routingJar struct{ jar *cookiejar.Jar }

func newRoutingJar() *routingJar {
	jar, _ := cookiejar.New(nil) // a nil Options never errors
	return &routingJar{jar: jar}
}

func (j *routingJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	kept := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if routingCookieNames[c.Name] {
			kept = append(kept, c)
		}
	}
	if len(kept) > 0 {
		j.jar.SetCookies(u, kept)
	}
}

func (j *routingJar) Cookies(u *url.URL) []*http.Cookie { return j.jar.Cookies(u) }

// routingCookies is shared by every Codex client in the process. Clients are
// rebuilt on a token refresh, a model switch, /clear and /resume; the cookie
// is the edge's pin and has to survive that.
var routingCookies = newRoutingJar()

// turnState holds the sticky-routing token for the turn in flight. The first
// value wins: the server may answer a later request of the turn with a fresh
// token, but the contract is to keep replaying the one the turn started with.
type turnState struct {
	mu    sync.Mutex
	value string
}

func (t *turnState) get() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.value
}

// capture records the token a response carries, unless the turn already has
// one.
func (t *turnState) capture(h http.Header) {
	v := h.Get(turnStateHeader)
	if v == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.value == "" {
		t.value = v
	}
}

func (t *turnState) reset() {
	t.mu.Lock()
	t.value = ""
	t.mu.Unlock()
}
