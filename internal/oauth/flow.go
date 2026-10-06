package oauth

import (
	"context"
	"fmt"
)

// Kind is the interactive OAuth style a provider signs in with.
type Kind int

const (
	// KindNone means the provider has no interactive OAuth login.
	KindNone Kind = iota
	// KindDeviceCode is RFC 8628: the user opens a verification page and types
	// the code the CLI printed.
	KindDeviceCode
	// KindBrowser is a loopback redirect: the browser hands the authorization
	// code back to a listener on this machine.
	KindBrowser
)

// KindFor reports providerID's interactive login style.
func KindFor(providerID string) Kind {
	switch providerID {
	case "xai":
		return KindDeviceCode
	case "openai":
		return KindBrowser
	default:
		return KindNone
	}
}

// Supports reports whether providerID has an interactive OAuth login.
func Supports(providerID string) bool {
	return KindFor(providerID) != KindNone
}

// Browser reports whether providerID signs in through the browser rather than
// a device code.
func Browser(providerID string) bool {
	return KindFor(providerID) == KindBrowser
}

// RotatesRefreshToken reports whether providerID's refresh token is single-use:
// the provider returns a new one and the redeemed token dies. A refresh
// response for such a provider is unusable without a replacement token.
func RotatesRefreshToken(providerID string) bool {
	switch providerID {
	case "xai":
		return true
	default:
		return false
	}
}

// Flow is one in-flight interactive authorization. The caller opens URL
// (showing UserCode when the flow has one), then blocks in Wait.
type Flow interface {
	// URL is the page the user opens. A device flow embeds the user code.
	URL() string
	// UserCode is the code the user types; empty for a browser flow.
	UserCode() string
	// Wait blocks until the user authorizes — returning tokens — the flow
	// fails, or ctx is done.
	Wait(ctx context.Context) (*TokenResponse, error)
	// Close releases local resources and unblocks a pending Wait. Safe to call
	// repeatedly and from another goroutine.
	Close()
}

// Begin starts the interactive login for providerID.
func Begin(ctx context.Context, providerID string) (Flow, error) {
	switch KindFor(providerID) {
	case KindDeviceCode:
		return xaiBeginDevice(ctx)
	case KindBrowser:
		return codexBeginBrowser()
	default:
		return nil, fmt.Errorf("oauth login not supported for provider %q", providerID)
	}
}

// Refresh exchanges a refresh_token for new tokens.
func Refresh(ctx context.Context, providerID, refreshToken string) (*TokenResponse, error) {
	switch providerID {
	case "xai":
		return xaiRefresh(ctx, refreshToken)
	case "openai":
		return codexRefresh(ctx, refreshToken)
	default:
		return nil, fmt.Errorf("oauth refresh not supported for provider %q", providerID)
	}
}
