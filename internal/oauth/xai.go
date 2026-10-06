package oauth

import (
	"context"
)

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

// deviceCodeResponse is the raw device authorization endpoint response.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"` // seconds
}

func (d deviceCodeResponse) toDeviceCode() DeviceCode {
	return DeviceCode(d)
}

// deviceFlow is a pending xAI device authorization: the user opens the
// verification page and types the code zeta prints.
type deviceFlow struct {
	device DeviceCode
}

func xaiBeginDevice(ctx context.Context) (Flow, error) {
	device, err := xaiStartDevice(ctx)
	if err != nil {
		return nil, err
	}
	return deviceFlow{device: device}, nil
}

func (f deviceFlow) URL() string      { return f.device.BrowserURL() }
func (f deviceFlow) UserCode() string { return f.device.UserCode }
func (deviceFlow) Close()             {}

func (f deviceFlow) Wait(ctx context.Context) (*TokenResponse, error) {
	return xaiPollDevice(ctx, f.device)
}
