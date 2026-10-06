package oauth

// CodexClientID is the public desktop OAuth client_id registered with OpenAI.
// Reused because the authorize endpoint is bound to it plus the redirect URI
// below: a self-registered client cannot request a ChatGPT-plan scoped token.
const CodexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// CodexOriginator identifies the client to OpenAI's authorize and Codex
// backend endpoints. The subscription-scoped consent screen and the
// chatgpt.com backend gate on it, so it has to match the registered client.
const CodexOriginator = "codex_cli_rs"

const (
	// CodexIssuer is the OAuth 2.0 authorization server.
	CodexIssuer = "https://auth.openai.com"
	// CodexScope is the scope the ChatGPT-plan login requests.
	CodexScope = "openid profile email offline_access"

	// CodexAuthorizeURL is the browser authorization endpoint.
	CodexAuthorizeURL = CodexIssuer + "/oauth/authorize"

	// CodexRedirectHost/Path complete the registered loopback redirect. The
	// host must be "localhost" — that is the spelling the client_id is
	// registered with, so the browser has to resolve it back to us.
	CodexRedirectHost = "localhost"
	CodexRedirectPath = "/auth/callback"
)

// CodexRedirectPort is the registered loopback port. The redirect URI is part
// of the client registration, so it cannot float; a var so tests can bind an
// ephemeral port instead.
var CodexRedirectPort = 1455

// CodexTokenURL is the OpenAI token endpoint (var so tests can redirect).
var CodexTokenURL = CodexIssuer + "/oauth/token"
