package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

// ownerBindings is an in-memory BindingSource keyed by owner. Gates let a test
// hold one tenant inside binding resolution while another tenant proceeds.
type ownerBindings struct {
	mu      sync.Mutex
	byOwner map[string]Binding
	errs    map[string]error
	gates   map[string]chan struct{}
	entered map[string]chan struct{}
	calls   int
}

func newOwnerBindings() *ownerBindings {
	return &ownerBindings{
		byOwner: make(map[string]Binding),
		errs:    make(map[string]error),
		gates:   make(map[string]chan struct{}),
		entered: make(map[string]chan struct{}),
	}
}

func (b *ownerBindings) BindingFor(ctx context.Context, owner app.Identity) (Binding, error) {
	key := ownerKey(owner)
	b.mu.Lock()
	b.calls++
	binding := b.byOwner[key]
	err := b.errs[key]
	gate := b.gates[key]
	entered := b.entered[key]
	b.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return Binding{}, ctx.Err()
		}
	}
	if err != nil {
		return Binding{}, err
	}
	if binding.RenderURL == "" {
		return Binding{}, &ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "请先绑定服务"}
	}
	return binding, nil
}

// ownerClients resolves fake clients by service address so a test can give two
// tenants separate downstream services.
type ownerClients struct {
	mu     sync.Mutex
	render map[string]RenderClient
}

func newOwnerClients() *ownerClients {
	return &ownerClients{render: make(map[string]RenderClient)}
}

func (c *ownerClients) Render(binding Binding) RenderClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.render[binding.RenderURL]
}

type tenantEnv struct {
	svc      *Service
	registry *app.Registry
	bindings *ownerBindings
	clients  *ownerClients
	builder  *fakeBuilder
	notifier *fakeNotifier
}

func newTenantEnv(t *testing.T) *tenantEnv {
	t.Helper()
	env := &tenantEnv{
		registry: app.NewRegistry(),
		bindings: newOwnerBindings(),
		clients:  newOwnerClients(),
		builder:  &fakeBuilder{},
		notifier: &fakeNotifier{},
	}
	env.svc = NewWithBindings(env.registry, env.builder, env.bindings, env.clients, env.notifier, Config{
		StartTimeout:      2 * time.Second,
		ProjectionTimeout: time.Second,
		OperationsTimeout: time.Second,
		MonitorInterval:   time.Hour,
		PortMin:           39000,
		PortMax:           39010,
	})
	return env
}

func (e *tenantEnv) addTenant(t *testing.T, owner app.Identity, renderClient *fakeRender) {
	t.Helper()
	key := ownerKey(owner)
	renderURL := "https://render-" + key + ".example"
	e.bindings.byOwner[key] = Binding{RenderURL: renderURL}
	e.clients.render[renderURL] = renderClient
}

func newFakeTenant(appID, instanceID string) *fakeRender {
	instance := readyApp()
	instance.AppID = appID
	instance.InstanceID = instanceID
	return &fakeRender{instance: instance, getInstance: instance}
}

func idleAppFor(t *testing.T, id string, owner app.Identity) app.App {
	t.Helper()
	value := idleApp(t, owner)
	value.ID = id
	return value
}

func TestStopAllForOwnerReportsFailuresWithoutBlocking(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	renderClient := newFakeTenant("app_a", "inst_a")
	renderClient.deleteErr = errors.New("render down")
	env.addTenant(t, owner, renderClient)
	running := runningApp(owner)
	running.ID = "app_a"
	env.registry.Add(running)

	failures := env.svc.StopAllForOwner(context.Background(), []app.Identity{owner})
	if len(failures) != 1 {
		t.Fatalf("failures = %v", failures)
	}
	if value, _ := env.registry.Get("app_a"); value.Snapshot().State != app.StateRunning {
		t.Fatalf("a failed stop must keep the runtime reservation: %+v", value.Snapshot())
	}
}

