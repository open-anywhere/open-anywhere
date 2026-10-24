package linx

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// APIURL is the Linx management host used to bind an activation code to an agent.
const APIURL = "https://xrobo.qiniu.com"

// NewIdentity creates a fresh virtual device identity: a locally administered
// unicast MAC and a random UUID v4 client id. Token and WSURL stay empty until
// the platform binds the device.
func NewIdentity() (Device, error) {
	m := make([]byte, 6)
	u := make([]byte, 16)
	if _, err := rand.Read(m); err != nil {
		return Device{}, err
	}
	if _, err := rand.Read(u); err != nil {
		return Device{}, err
	}
	m[0] = 0x02
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return Device{
		MAC:      fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", m[0], m[1], m[2], m[3], m[4], m[5]),
		ClientID: fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]),
	}, nil
}

// SaveDevice writes the credential file readable only by the current user.
func SaveDevice(path string, d Device) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Bind attaches the device behind an activation code to an agent. The API key
// is only sent as the Authorization header and never included in errors.
func (c OTAClient) Bind(ctx context.Context, apiKey, agentID, code string) error {
	base := c.APIBase
	if base == "" {
		base = APIURL
	}
	u := strings.TrimRight(base, "/") + "/xiaozhi/device/bind/" + url.PathEscape(agentID) + "/" + url.PathEscape(code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("bind request: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	parsed := json.Unmarshal(b, &out) == nil
	if res.StatusCode < 200 || res.StatusCode >= 300 || (parsed && out.Code != 0) {
		return fmt.Errorf("bind failed: HTTP %d code=%d %s", res.StatusCode, out.Code, out.Msg)
	}
	return nil
}
