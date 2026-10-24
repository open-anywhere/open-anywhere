package dlna

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

func soapResult(action, state string) string {
	return `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + `Response xmlns:u="` + avTransport + `"><CurrentTransportState>` + state + `</CurrentTransportState></u:` + action + `Response></s:Body></s:Envelope>`
}
func actionOf(r *http.Request) string {
	return strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("SOAPAction"), `"`+avTransport+`#`), `"`)
}

func TestTransportControlAndMetadata(t *testing.T) {
	var actions []string
	source := api.StreamSource{URL: "http://media.invalid/live.ts?a=1&b=<tag>", Format: "mpegts", VideoCodec: "h264", AudioCodec: "aac"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := actionOf(r)
		actions = append(actions, action)
		if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/xml") {
			t.Error("invalid SOAP method or content type")
		}
		var env struct {
			Body struct {
				Action struct {
					InstanceID string `xml:"InstanceID"`
					URI        string `xml:"CurrentURI"`
					Metadata   string `xml:"CurrentURIMetaData"`
					Speed      string `xml:"Speed"`
				} `xml:",any"`
			} `xml:"Body"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Error(err)
		}
		if env.Body.Action.InstanceID != "0" {
			t.Error("invalid InstanceID")
		}
		if action == "SetAVTransportURI" {
			if env.Body.Action.URI != source.URL {
				t.Errorf("URI did not survive XML escaping: %q", env.Body.Action.URI)
			}
			var didl struct {
				Item struct {
					Res struct {
						Protocol string `xml:"protocolInfo,attr"`
						URL      string `xml:",chardata"`
					} `xml:"res"`
				} `xml:"item"`
			}
			if err := xml.Unmarshal([]byte(env.Body.Action.Metadata), &didl); err != nil {
				t.Error(err)
			}
			if didl.Item.Res.Protocol != "http-get:*:video/mp2t:*" || didl.Item.Res.URL != source.URL {
				t.Errorf("invalid metadata: %+v", didl)
			}
		}
		if action == "Play" && env.Body.Action.Speed != "1" {
			t.Error("invalid speed")
		}
		fmt.Fprint(w, soapResult(action, "PLAYING"))
	}))
	defer server.Close()
	transport := &Transport{Client: server.Client()}
	device := core.Device{ControlURL: server.URL}
	if err := transport.Start(context.Background(), device, source); err != nil {
		t.Fatal(err)
	}
	state, err := transport.Status(context.Background(), device)
	if err != nil || state != api.SessionPlaying {
		t.Fatalf("%s %v", state, err)
	}
	if err := transport.Stop(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actions, []string{"SetAVTransportURI", "Play", "GetTransportInfo", "Stop"}) {
		t.Fatal(actions)
	}
}
func TestTransportStateMapping(t *testing.T) {
	for input, want := range map[string]api.SessionState{"PLAYING": api.SessionPlaying, "TRANSITIONING": api.SessionStarting, "STOPPED": api.SessionStopped, "NO_MEDIA_PRESENT": api.SessionStopped, "PAUSED_PLAYBACK": api.SessionPaused} {
		t.Run(input, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, soapResult("GetTransportInfo", input)) }))
			defer server.Close()
			got, err := (&Transport{}).Status(context.Background(), core.Device{ControlURL: server.URL})
			if err != nil || got != want {
				t.Fatalf("%s %v", got, err)
			}
		})
	}
}
func TestTransportErrors(t *testing.T) {
	fault := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>714</errorCode><errorDescription>Illegal MIME-type</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	for _, tt := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"SOAP fault", fault, 500, "714 Illegal MIME-type"},
		{"fault even with HTTP 200", fault, 200, "714"},
		{"HTTP error", soapResult("Stop", ""), 503, "HTTP 503"},
		{"invalid XML", "<broken", 200, "invalid SOAP"},
		{"wrong envelope", "<html>error</html>", 200, "invalid SOAP"},
		{"missing response", `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body/></s:Envelope>`, 200, "missing SOAP action response"},
		{"unknown state", soapResult("GetTransportInfo", "GARBAGE"), 200, "unknown transport state"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) }))
			defer server.Close()
			_, err := (&Transport{}).Status(context.Background(), core.Device{ControlURL: server.URL})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
	t.Run("request canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := (&Transport{}).Stop(ctx, core.Device{ControlURL: "http://127.0.0.1:1"}); err == nil {
			t.Fatal("canceled request succeeded")
		}
	})
	t.Run("bad URL", func(t *testing.T) {
		if err := (&Transport{}).Stop(context.Background(), core.Device{ControlURL: "http://["}); err == nil {
			t.Fatal("bad URL accepted")
		}
	})
	t.Run("failed set never plays", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, fault) }))
		defer server.Close()
		err := (&Transport{}).Start(context.Background(), core.Device{ControlURL: server.URL}, api.StreamSource{URL: "http://media.invalid/live.ts"})
		if err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	})
}

// This runs the public API against a simulated television that actually pulls
// media, verifies SOAP control order, reports playback, and handles Stop.
func TestAPIToTelevisionIntegration(t *testing.T) {
	mediaBody := strings.Repeat("\x47"+strings.Repeat("\x00", 187), 10)
	pulled := make(chan struct{}, 1)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		fmt.Fprint(w, mediaBody)
		pulled <- struct{}{}
	}))
	defer media.Close()
	state := "STOPPED"
	tv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := actionOf(r)
		switch action {
		case "SetAVTransportURI":
			var env struct {
				Body struct {
					Set struct {
						URI string `xml:"CurrentURI"`
					} `xml:"SetAVTransportURI"`
				} `xml:"Body"`
			}
			if err := xml.NewDecoder(r.Body).Decode(&env); err != nil {
				t.Error(err)
			}
			resp, err := http.Get(env.Body.Set.URI)
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(data) != mediaBody {
				t.Error("media not passed intact")
			}
		case "Play":
			state = "PLAYING"
		case "Stop":
			state = "STOPPED"
		}
		fmt.Fprint(w, soapResult(action, state))
	}))
	defer tv.Close()
	registry := core.NewRegistry()
	registry.Upsert(core.Device{Target: api.Target{ID: "tv", Name: "Virtual TV", State: api.TargetOnline, Endpoints: []api.Endpoint{{ID: "dlna-renderer", Protocol: "dlna", Capabilities: api.Capabilities{Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Streaming: true}}}}, ControlURL: tv.URL})
	service := core.NewService(registry, map[string]core.Adapter{"dlna": &Transport{Client: tv.Client()}})
	server := httptest.NewServer(core.NewHandler(service))
	defer server.Close()
	body := fmt.Sprintf(`{"target_id":"tv","endpoint_id":"dlna-renderer","source":{"type":"stream","url":%q,"format":"mpegts","video_codec":"h264","audio_codec":"aac"}}`, media.URL+"/live.ts")
	resp, err := http.Post(server.URL+"/v1/projection-sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var session api.Session
	// JSON decoding deliberately exercises the public contract through HTTP.
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("%d %s", resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.State != api.SessionPlaying {
		t.Fatal(session.State)
	}
	select {
	case <-pulled:
	case <-time.After(time.Second):
		t.Fatal("TV never pulled media")
	}
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/v1/projection-sessions/"+session.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(data), `"state":"stopped"`) {
		t.Fatalf("%d %s", resp.StatusCode, data)
	}
}

func TestTransportRejectsTruncatedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		fmt.Fprint(w, "<incomplete")
	}))
	defer server.Close()
	err := (&Transport{Client: server.Client()}).Stop(context.Background(), core.Device{ControlURL: server.URL})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want body read error, got %v", err)
	}
}
