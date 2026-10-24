package mocktv

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestAVTransportBodyReadFailure(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	request := httptest.NewRequest(http.MethodPost, "/control/avtransport", nil)
	request.Body = io.NopCloser(failingReader{})
	recorder := httptest.NewRecorder()
	display.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAVTransportMissingAction(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	for _, body := range []string{"", "not xml", "<a/>", "<a><b/></a>"} {
		request := httptest.NewRequest(http.MethodPost, "/control/avtransport", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		display.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("body %q: status = %d, want 500", body, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "Invalid Action") {
			t.Fatalf("body %q: fault = %s", body, recorder.Body.String())
		}
	}
}

func TestAVTransportActionFromBody(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">` +
		`<CurrentURI> http://127.0.0.1:9/live.ts </CurrentURI>` +
		`</u:SetAVTransportURI></s:Body></s:Envelope>`
	request := httptest.NewRequest(http.MethodPost, "/control/avtransport", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	display.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "SetAVTransportURIResponse") {
		t.Fatalf("response = %s", recorder.Body.String())
	}
	if state := display.State(); state.State != "cued" || state.URL != "http://127.0.0.1:9/live.ts" {
		t.Fatalf("state = %+v", state)
	}
}

func TestAVTransportUnknownBodyAction(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:GetVolume xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"/></s:Body></s:Envelope>`
	request := httptest.NewRequest(http.MethodPost, "/control/avtransport", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	display.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestSoapActionHeaderForms(t *testing.T) {
	envelope := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:Play xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"/></s:Body></s:Envelope>`
	cases := []struct {
		name   string
		header string
		body   string
		want   string
	}{
		{"quoted action", `"urn:schemas-upnp-org:service:AVTransport:1#Play"`, "", "Play"},
		{"unquoted action", `urn:schemas-upnp-org:service:AVTransport:1#Stop`, "", "Stop"},
		{"trailing hash falls back to body", `urn:schemas-upnp-org:service:AVTransport:1#`, envelope, "Play"},
		{"header without hash falls back to body", `urn:schemas-upnp-org:service:AVTransport:1`, envelope, "Play"},
		{"empty header uses body", "", envelope, "Play"},
		{"whitespace only header uses body", "   ", envelope, "Play"},
		{"malformed xml", "", `<s:Envelope`, ""},
		{"shallow xml", "", `<a><b/></a>`, ""},
		{"empty body", "", "", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := soapAction(testCase.header, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("soapAction(%q) = %q, want %q", testCase.header, got, testCase.want)
			}
		})
	}
}

func TestCurrentURIEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing current uri", `<a><b/></a>`, ""},
		{"malformed xml", `<a><CurrentURI>`, ""},
		{"mismatched element", `<a><CurrentURI><b></CurrentURI></a>`, ""},
		{"unescaped entities", `<a><CurrentURI>x &amp; y</CurrentURI></a>`, "x & y"},
		{"blank value", `<a><CurrentURI>   </CurrentURI></a>`, ""},
		{"empty body", "", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := currentURI([]byte(testCase.body)); got != testCase.want {
				t.Fatalf("currentURI = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSOAPFaultEscapesDescription(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeSOAPFault(recorder, 402, `<bad & "value">`)
	body := recorder.Body.String()
	if !strings.Contains(body, "&lt;bad &amp; &#34;value&#34;&gt;") {
		t.Fatalf("fault description was not escaped: %s", body)
	}
	if !strings.Contains(body, "<errorCode>402</errorCode>") {
		t.Fatalf("fault code missing: %s", body)
	}
}
