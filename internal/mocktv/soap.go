package mocktv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// handleAVTransport accepts the SOAP actions the casting service sends to a
// DLNA MediaRenderer.
func (d *Display) handleAVTransport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid SOAP body", http.StatusBadRequest)
		return
	}
	action := soapAction(r.Header.Get("SOAPAction"), body)
	if action == "" {
		writeSOAPFault(w, 401, "Invalid Action")
		return
	}
	switch action {
	case "SetAVTransportURI":
		d.cue(currentURI(body))
		writeSOAP(w, action, "")
	case "Play":
		d.play()
		writeSOAP(w, action, "")
	case "Stop":
		d.stop()
		writeSOAP(w, action, "")
	case "GetTransportInfo":
		writeSOAP(w, action, d.transportState())
	default:
		d.logf("mock-tv: unsupported SOAP action %q", action)
		writeSOAPFault(w, 401, "Invalid Action")
	}
}

func soapAction(header string, body []byte) string {
	header = strings.TrimSpace(strings.Trim(header, `"`))
	if index := strings.LastIndex(header, "#"); index >= 0 && index+1 < len(header) {
		return header[index+1:]
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 3 {
				return value.Name.Local
			}
		case xml.EndElement:
			depth--
		}
	}
}

func currentURI(body []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "CurrentURI" {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
}

func writeSOAP(w http.ResponseWriter, action, state string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	body.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:`)
	body.WriteString(action)
	body.WriteString(`Response xmlns:u="`)
	body.WriteString(avTransport)
	body.WriteString(`">`)
	if state != "" {
		body.WriteString(`<CurrentTransportState>`)
		body.WriteString(state)
		body.WriteString(`</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus><CurrentSpeed>1</CurrentSpeed>`)
	}
	body.WriteString(`</u:`)
	body.WriteString(action)
	body.WriteString(`Response></s:Body></s:Envelope>`)
	_, _ = io.WriteString(w, body.String())
}

func writeSOAPFault(w http.ResponseWriter, code int, description string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code, xmlEscape(description))
}
