// Package dlna implements UPnP MediaRenderer discovery and control.
package dlna

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

const avTransport = "urn:schemas-upnp-org:service:AVTransport:1"

type Transport struct{ Client *http.Client }

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (t *Transport) call(ctx context.Context, d core.Device, action, args string) (string, error) {
	body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + avTransport + `"><InstanceID>0</InstanceID>` + args + `</u:` + action + `></s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.ControlURL, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+avTransport+`#`+action+`"`)
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var envelope struct {
		XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
		Body    struct {
			Fault *struct {
				Code        string `xml:"detail>UPnPError>errorCode"`
				Description string `xml:"detail>UPnPError>errorDescription"`
			} `xml:"Fault"`
			Result *struct {
				XMLName xml.Name
				State   string `xml:"CurrentTransportState"`
			} `xml:",any"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", fmt.Errorf("%s: invalid SOAP response: %w", action, err)
	}
	if f := envelope.Body.Fault; f != nil {
		return "", fmt.Errorf("%s: UPnP %s %s", action, f.Code, f.Description)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: HTTP %d", action, resp.StatusCode)
	}
	if envelope.Body.Result == nil || envelope.Body.Result.XMLName.Local != action+"Response" {
		return "", fmt.Errorf("%s: missing SOAP action response", action)
	}
	return envelope.Body.Result.State, nil
}

func (t *Transport) Start(ctx context.Context, d core.Device, src api.StreamSource) error {
	metadata := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="0" parentID="-1" restricted="1"><dc:title>Live stream</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo="http-get:*:video/mp2t:*">` + escape(src.URL) + `</res></item></DIDL-Lite>`
	if _, err := t.call(ctx, d, "SetAVTransportURI", "<CurrentURI>"+escape(src.URL)+"</CurrentURI><CurrentURIMetaData>"+escape(metadata)+"</CurrentURIMetaData>"); err != nil {
		return err
	}
	_, err := t.call(ctx, d, "Play", "<Speed>1</Speed>")
	return err
}
func (t *Transport) Stop(ctx context.Context, d core.Device) error {
	_, err := t.call(ctx, d, "Stop", "")
	return err
}
func (t *Transport) Status(ctx context.Context, d core.Device) (api.SessionState, error) {
	state, err := t.call(ctx, d, "GetTransportInfo", "")
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(state) {
	case "PLAYING":
		return api.SessionPlaying, nil
	case "PAUSED_PLAYBACK":
		return api.SessionPaused, nil
	case "STOPPED", "NO_MEDIA_PRESENT":
		return api.SessionStopped, nil
	case "TRANSITIONING":
		return api.SessionStarting, nil
	default:
		return "", fmt.Errorf("unknown transport state %q", state)
	}
}
