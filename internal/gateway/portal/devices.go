package portal

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const (
	// maxDevicesPerUser bounds how many Linx agents and bound devices one
	// account can create on the platform's Linx key.
	maxDevicesPerUser = 10

	maxDeviceNameRunes = 40
	maxActivationCode  = 32
)

type createDeviceRequest struct {
	// Kind is "virtual" (a new agent driven from the browser) or "physical"
	// (hardware bound to an agent the user already has). Empty means virtual.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// ActivationCode is the code the hardware shows after it connects.
	ActivationCode string `json:"activation_code"`
	// AgentDeviceID picks which of the user's agents the hardware joins. Empty
	// uses the first device that has an agent.
	AgentDeviceID string `json:"agent_device_id"`
}

func cleanDeviceName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxDeviceNameRunes {
		return "", invalidArgument("设备名称最长 40 个字符")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", invalidArgument("设备名称不能包含控制字符")
		}
	}
	return value, nil
}

func cleanActivationCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalidArgument("请填写设备激活码")
	}
	if len(value) > maxActivationCode {
		return "", invalidArgument("设备激活码格式不正确")
	}
	for _, r := range value {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return "", invalidArgument("设备激活码只能包含字母和数字")
		}
	}
	return value, nil
}

func (a *API) createDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	var in createDeviceRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	name, err := cleanDeviceName(in.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	devices, err := a.db.PortalDevicesByUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(devices) >= maxDevicesPerUser {
		writeError(w, apiError(http.StatusConflict, "DEVICE_LIMIT", "设备数量已达上限，请先移除不用的设备"))
		return
	}
	switch in.Kind {
	case "", store.PortalDeviceVirtual:
		a.createVirtualDevice(w, r, user, name)
	case store.PortalDevicePhysical:
		a.createPhysicalDevice(w, r, user, name, in, devices)
	default:
		writeError(w, invalidArgument("设备类型只能是 virtual 或 physical"))
	}
}

func (a *API) createVirtualDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser, name string) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	identity, err := NewDeviceIdentity()
	if err != nil {
		writeError(w, internalError("无法生成设备身份"))
		return
	}
	now := a.now()
	device := store.PortalDevice{
		ID:        "dev_" + randText(),
		UserID:    user.ID,
		Kind:      store.PortalDeviceVirtual,
		MAC:       identity.MAC,
		ClientID:  identity.ClientID,
		Name:      name,
		State:     store.PortalDeviceProvisioning,
		Step:      stepCreateAgent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.db.CreatePortalDevice(r.Context(), device); err != nil {
		writeError(w, err)
		return
	}
	a.provision.Start(device.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"device": a.deviceView(device)})
}

func (a *API) createPhysicalDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser, name string, in createDeviceRequest, devices []store.PortalDevice) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	code, err := cleanActivationCode(in.ActivationCode)
	if err != nil {
		writeError(w, err)
		return
	}
	var base *store.PortalDevice
	for index := range devices {
		candidate := &devices[index]
		if candidate.LinxAgentID == "" {
			continue
		}
		if in.AgentDeviceID == "" || candidate.ID == in.AgentDeviceID {
			base = candidate
			break
		}
	}
	if base == nil {
		if in.AgentDeviceID != "" {
			writeError(w, errNotFound)
			return
		}
		writeError(w, apiError(http.StatusConflict, "AGENT_NOT_READY", "智能体还没有开通完成，请稍后再添加硬件设备"))
		return
	}
	mac, err := a.provision.BindHardware(r.Context(), base.LinxAgentID, code)
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	now := a.now()
	device := store.PortalDevice{
		ID:           "dev_" + randText(),
		UserID:       user.ID,
		Kind:         store.PortalDevicePhysical,
		MAC:          mac,
		LinxAgentID:  base.LinxAgentID,
		AgentName:    base.AgentName,
		Name:         name,
		State:        store.PortalDeviceRegistered,
		Step:         stepDone,
		CreatedAt:    now,
		UpdatedAt:    now,
		RegisteredAt: &now,
	}
	if err := a.db.CreatePortalDevice(r.Context(), device); err != nil {
		if isUniqueViolation(err) {
			writeError(w, apiError(http.StatusConflict, "DEVICE_EXISTS", "该硬件设备已经添加过了"))
			return
		}
		writeError(w, err)
		return
	}
	a.logf("portal: user %s bound hardware %s to agent %s", user.ID, device.ID, device.LinxAgentID)
	writeJSON(w, http.StatusCreated, map[string]any{"device": a.deviceView(device)})
}

// mapBindError turns a Linx failure (activation, voices, agent settings) into a message people can act on.
func mapBindError(err error) error {
	var apiErr *linx.Error
	switch {
	case errors.Is(err, app.ErrPlatformNotConfigured):
		return apiError(http.StatusConflict, "PLATFORM_NOT_CONFIGURED", "平台尚未配置，请联系维护者")
	case linx.IsAuth(err):
		return apiError(http.StatusBadGateway, "PLATFORM_AUTH_FAILED", "平台的灵矽 API Key 无效，请联系维护者")
	case linx.IsUnavailable(err):
		return apiError(http.StatusBadGateway, "PLATFORM_UNAVAILABLE", "灵矽平台暂时不可用，请稍后重试")
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
		return apiError(http.StatusConflict, "DEVICE_ALREADY_BOUND", "该设备已被绑定，请先在灵矽控制台解绑，或确认激活码")
	case linx.IsBindRejected(err):
		return apiError(http.StatusBadRequest, "ACTIVATION_CODE_INVALID", "激活码无效或已使用，请让设备重新联网获取新的激活码")
	}
	return apiError(http.StatusBadGateway, "PLATFORM_ERROR", "调用灵矽平台失败，请稍后重试")
}

// deleteDevice removes a device. Virtual devices lose their voice runtime and
// credentials; hardware is released from the Linx account first so its
// activation code can be used again. The Linx agent itself is left in place.
func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.ownedDevice(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	devices, err := a.db.PortalDevicesByUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(devices) <= 1 {
		writeError(w, apiError(http.StatusConflict, "LAST_DEVICE", "至少需要保留一台设备"))
		return
	}
	if device.State == store.PortalDeviceProvisioning && a.provision != nil && a.provision.Running(device.ID) {
		writeError(w, apiError(http.StatusConflict, "DEVICE_BUSY", "设备正在开通中，请稍后再试"))
		return
	}
	if device.LinxAgentID != "" {
		for _, other := range devices {
			if other.ID != device.ID && other.Kind == store.PortalDevicePhysical && other.LinxAgentID == device.LinxAgentID &&
				device.Kind == store.PortalDeviceVirtual {
				writeError(w, apiError(http.StatusConflict, "DEVICE_HAS_HARDWARE", "请先移除绑定在这个智能体上的硬件设备"))
				return
			}
		}
		if a.apps != nil {
			summaries, err := a.apps.ListApps(r.Context(), app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID})
			if err != nil {
				writeError(w, err)
				return
			}
			if len(summaries) > 0 {
				writeError(w, apiError(http.StatusConflict, "APP_BUSY", "这台设备下还有应用，请先删除应用"))
				return
			}
		}
	}
	if device.Kind == store.PortalDevicePhysical && a.provision != nil {
		if err := a.provision.UnbindHardware(r.Context(), device.MAC); err != nil {
			a.logf("portal: unbind hardware %s: %v", device.ID, err)
			if r.URL.Query().Get("force") != "true" {
				writeError(w, apiError(http.StatusBadGateway, "UNBIND_FAILED",
					"向灵矽解绑设备失败，请稍后重试；如果已在灵矽控制台解绑，可以强制移除"))
				return
			}
		}
	}
	a.removeDeviceRuntime(r.Context(), device)
	if err := a.db.DeletePortalDevice(r.Context(), device.ID); err != nil {
		writeError(w, err)
		return
	}
	a.logf("portal: user %s removed device %s", user.ID, device.ID)
	w.WriteHeader(http.StatusNoContent)
}

// removeDeviceRuntime stops the browser voice runtime and deletes the stored
// credential of a virtual device.
func (a *API) removeDeviceRuntime(_ context.Context, device store.PortalDevice) {
	if device.Kind != store.PortalDeviceVirtual {
		return
	}
	if a.voice != nil {
		a.voice.Stop(device.ID)
	}
	if a.provision == nil {
		return
	}
	path := a.provision.CredentialPath(device)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.logf("portal: remove credential for %s: %v", device.ID, err)
	}
}
