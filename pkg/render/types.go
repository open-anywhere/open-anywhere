// Package render defines the HTTP contract between the gateway and Render.
package render

import "github.com/open-anywhere/open-anywhere/pkg/casting"

type CreateRequest struct {
	AppID       string `json:"app_id"`
	ArtifactURL string `json:"artifact_url"`
	Port        int    `json:"port"`
}

type Error struct {
	Stage   string `json:"stage,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Instance struct {
	AppID      string                `json:"app_id"`
	InstanceID string                `json:"instance_id"`
	State      string                `json:"state"`
	Port       int                   `json:"port"`
	AppBaseURL string                `json:"app_base_url,omitempty"`
	Source     *casting.StreamSource `json:"source,omitempty"`
	Error      *Error                `json:"error,omitempty"`
}

type Port struct {
	Port       int     `json:"port"`
	Occupied   bool    `json:"occupied"`
	Owner      *string `json:"owner"`
	AppID      string  `json:"app_id,omitempty"`
	InstanceID string  `json:"instance_id,omitempty"`
}
