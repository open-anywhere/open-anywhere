package dlna

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

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
	device := Device{ControlURL: server.URL}
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
			got, err := (&Transport{}).Status(context.Background(), Device{ControlURL: server.URL})
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
			_, err := (&Transport{}).Status(context.Background(), Device{ControlURL: server.URL})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
	t.Run("request canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := (&Transport{}).Stop(ctx, Device{ControlURL: "http://127.0.0.1:1"}); err == nil {
			t.Fatal("canceled request succeeded")
		}
	})
	t.Run("bad URL", func(t *testing.T) {
		if err := (&Transport{}).Stop(context.Background(), Device{ControlURL: "http://["}); err == nil {
			t.Fatal("bad URL accepted")
		}
	})
	t.Run("failed set never plays", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, fault) }))
		defer server.Close()
		err := (&Transport{}).Start(context.Background(), Device{ControlURL: server.URL}, api.StreamSource{URL: "http://media.invalid/live.ts"})
		if err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	})
}

func TestTransportRejectsTruncatedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		fmt.Fprint(w, "<incomplete")
	}))
	defer server.Close()
	err := (&Transport{Client: server.Client()}).Stop(context.Background(), Device{ControlURL: server.URL})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want body read error, got %v", err)
	}
}
