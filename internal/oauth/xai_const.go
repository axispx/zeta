package oauth

// XaiClientID is the public desktop OAuth client_id registered with xAI.
// Reused because xAI rejects loopback OAuth from non-allowlisted clients.
const XaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"

const (
	XaiDeviceAuthorizeURL = "https://auth.x.ai/oauth2/device/code"
	XaiScope              = "openid profile email offline_access grok-cli:access api:access"
	XaiDeviceGrantType    = "urn:ietf:params:oauth:grant-type:device_code"
)

// XaiTokenURL is the xAI token endpoint (var so tests can redirect).
var XaiTokenURL = "https://auth.x.ai/oauth2/token"
