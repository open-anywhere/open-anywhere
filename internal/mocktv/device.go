package mocktv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
)

const (
	mediaRenderer = "urn:schemas-upnp-org:device:MediaRenderer:1"
	avTransport   = "urn:schemas-upnp-org:service:AVTransport:1"
)

const deviceDescription = `<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>%s</friendlyName>
    <manufacturer>Open Anywhere</manufacturer>
    <modelName>mock-tv</modelName>
    <modelNumber>0.1.0</modelNumber>
    <UDN>%s</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:AVTransport</serviceId>
        <controlURL>/control/avtransport</controlURL>
        <eventSubURL>/event/avtransport</eventSubURL>
        <SCPDURL>/avtransport.xml</SCPDURL>
      </service>
    </serviceList>
  </device>
</root>
`

func (d *Display) handleDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprintf(w, deviceDescription, xmlEscape(d.cfg.Name), xmlEscape(d.cfg.UDN))
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
