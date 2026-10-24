package linx

import (
	"encoding/json"
	"errors"
	"os"
)

// Device contains the credentials returned by OTA for a bound virtual device.
type Device struct {
	MAC      string `json:"mac"`
	ClientID string `json:"clientId"`
	Token    string `json:"token"`
	WSURL    string `json:"wsUrl"`
}

func LoadDevice(path string) (Device, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Device{}, err
	}
	var d Device
	if err := json.Unmarshal(b, &d); err != nil {
		return Device{}, err
	}
	if d.MAC == "" || d.ClientID == "" || d.Token == "" || d.WSURL == "" {
		return Device{}, errors.New("device file is missing mac, clientId, token, or wsUrl")
	}
	return d, nil
}
