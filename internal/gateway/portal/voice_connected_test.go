package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestVoiceManagerWaitForConnectionSeesAConnectedRuntime(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := context.Background()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		greeting, err := json.Marshal(map[string]any{
			"type": "hello", "session_id": "session-1",
			"audio_params": map[string]any{"sample_rate": 16000},
		})
		if err != nil {
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, greeting); err != nil {
			return
		}
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	credentialPath := filepath.Join(env.dataDir, "devices", env.device.ID+".json")
	if err := saveCredential(credentialPath, devicelinx.Device{
		MAC: env.device.MAC, ClientID: env.device.ClientID, Token: "device-token",
		WSURL: "ws" + server.URL[len("http"):],
	}); err != nil {
		t.Fatal(err)
	}
	device := env.device
	device.State = store.PortalDeviceRegistered
	device.CredentialPath = credentialPath
	if err := env.voice.Start(device); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Cancel the runtime before the server cleanup waits for its handler.
	defer env.voice.StopAll()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !env.voice.WaitForConnection(ctx, device.ID, 5*time.Millisecond) {
		t.Fatal("the runtime never reported a connected session")
	}
}
