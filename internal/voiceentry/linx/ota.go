package linx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const OTAURL = "https://xrobo.qiniuapi.com/v1/ota/"

type OTAClient struct {
	BaseURL    string
	APIBase    string // management host for Bind; defaults to APIURL
	HTTPClient *http.Client
}

type OTAReport struct {
	Version       int            `json:"version"`
	UUID          string         `json:"uuid"`
	Application   map[string]any `json:"application"`
	Board         map[string]any `json:"board"`
	MACAddress    string         `json:"mac_address"`
	ChipModelName string         `json:"chip_model_name"`
}

type OTAResponse struct {
	Activation *struct {
		Code string `json:"code"`
	} `json:"activation,omitempty"`
	WebSocket *struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"websocket,omitempty"`
	Error string `json:"error,omitempty"`
}

func (c OTAClient) Report(ctx context.Context, d Device) (OTAResponse, error) {
	base := c.BaseURL
	if base == "" {
		base = OTAURL
	}
	report := OTAReport{Version: 0, UUID: d.ClientID, Application: map[string]any{"name": "open-anywhere-client", "version": "0.1.0", "elf_sha256": strings.Repeat("0", 64)}, Board: map[string]any{"type": "open-anywhere-client", "ssid": "mock", "rssi": 0, "mac": d.MAC}, MACAddress: d.MAC, ChipModelName: "esp32s3"}
	b, err := json.Marshal(report)
	if err != nil {
		return OTAResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/", strings.NewReader(string(b)))
	if err != nil {
		return OTAResponse{}, err
	}
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", d.MAC)
	req.Header.Set("Client-Id", d.ClientID)
	req.Header.Set("User-Agent", "open-anywhere-client/0.1.0")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return OTAResponse{}, err
	}
	defer res.Body.Close()
	var out OTAResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return out, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || out.Error != "" {
		return out, fmt.Errorf("OTA report failed: HTTP %d: %s", res.StatusCode, out.Error)
	}
	return out, nil
}
