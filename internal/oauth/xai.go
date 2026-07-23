// Package oauth implements provider OAuth 2.0 flows (currently xAI).
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Device-code polling signals from the token endpoint.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrPasteClosed          = errors.New("oauth paste closed")
)

// Supports reports whether providerID has an OAuth login path.
func Supports(providerID string) bool {
	return providerID == "xai"
}

// PkceCodes holds the verifier and challenge for a PKCE exchange.
type PkceCodes struct {
	Verifier  string
	Challenge string
	Method    string // always "S256"
}

// GeneratePKCE creates a new PKCE verifier and its S256 challenge.
func GeneratePKCE() (*PkceCodes, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, fmt.Errorf("pkce rand: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PkceCodes{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// GenerateState returns a random URL-safe string for OAuth state / nonce.
func GenerateState() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic("rand.Read: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// TokenResponse is the OAuth token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"` // "bearer"
	Scope        string `json:"scope,omitempty"`

	Error string `json:"error,omitempty"`
}

// DeviceCode is a pending device authorization (RFC 8628).
type DeviceCode struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               int64
	Interval                int64 // seconds
}

// BrowserURL is the URL to open for the user during device auth.
func (d DeviceCode) BrowserURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// DeviceCodeResponse is the raw device authorization endpoint response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"` // seconds
}

func (d DeviceCodeResponse) toDeviceCode() DeviceCode {
	return DeviceCode{
		DeviceCode:              d.DeviceCode,
		UserCode:                d.UserCode,
		VerificationURI:         d.VerificationURI,
		VerificationURIComplete: d.VerificationURIComplete,
		ExpiresIn:               d.ExpiresIn,
		Interval:                d.Interval,
	}
}
