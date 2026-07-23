package oauth

import "fmt"

// XaiClientID is the public desktop OAuth client_id registered with xAI.
// Reused because xAI rejects loopback OAuth from non-allowlisted clients.
const XaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"

const (
	XaiAuthorizeURL       = "https://auth.x.ai/oauth2/authorize"
	XaiDeviceAuthorizeURL = "https://auth.x.ai/oauth2/device/code"
	XaiRedirectHost       = "127.0.0.1"
	XaiRedirectPort       = 56121
	XaiRedirectPath       = "/callback"
	XaiScope              = "openid profile email offline_access grok-cli:access api:access"
	XaiDeviceGrantType    = "urn:ietf:params:oauth:grant-type:device_code"
)

// XaiTokenURL is the xAI token endpoint (var so tests can redirect).
var XaiTokenURL = "https://auth.x.ai/oauth2/token"

// XaiRedirectURI is the registered redirect_uri for the public Grok client.
// Sent in authorize/token requests; zeta does not listen on this address —
// xAI shows a pasteable code instead of completing a loopback redirect.
var XaiRedirectURI = fmt.Sprintf("http://%s:%d%s", XaiRedirectHost, XaiRedirectPort, XaiRedirectPath)
