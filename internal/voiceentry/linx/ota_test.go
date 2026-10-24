package linx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOTAReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ota/" || r.Header.Get("Activation-Version") != "1" {
			t.Errorf("request %s headers=%v", r.URL.Path, r.Header)
		}
		var body OTAReport
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MACAddress != "02:00:00:00:00:01" {
			t.Errorf("mac=%s", body.MACAddress)
		}
		_ = json.NewEncoder(w).Encode(OTAResponse{WebSocket: &struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}{URL: "ws://example", Token: "token"}})
	}))
	defer server.Close()
	c := OTAClient{BaseURL: server.URL + "/v1/ota"}
	d := Device{MAC: "02:00:00:00:00:01", ClientID: "client"}
	res, err := c.Report(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if res.WebSocket == nil || res.WebSocket.Token != "token" {
		t.Fatalf("response=%+v", res)
	}
}

func TestNewIdentityAndBind(t *testing.T) {
	d, err := NewIdentity()
	if err != nil || len(d.MAC) != 17 || d.MAC[:2] != "02" || len(d.ClientID) != 36 || d.ClientID[14] != '4' {
		t.Fatalf("identity=%+v err=%v", d, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xiaozhi/device/bind/agent1/123456" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			_, _ = w.Write([]byte(`{"code":401,"msg":"unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer server.Close()
	c := OTAClient{APIBase: server.URL}
	if err := c.Bind(context.Background(), "key", "agent1", "123456"); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind(context.Background(), "wrong", "agent1", "123456"); err == nil {
		t.Fatal("bad key accepted")
	}
}
