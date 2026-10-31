package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
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
	if binding.RenderURL == "" || binding.CastingURL == "" {
		return Binding{}, &ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "请先绑定服务"}
	}
	return binding, nil
}

// ownerClients resolves fake clients by service address so a test can give two
// tenants separate downstream services even when their screen IDs match.
type ownerClients struct {
	mu      sync.Mutex
	render  map[string]RenderClient
	casting map[string]CastingClient
}

func newOwnerClients() *ownerClients {
	return &ownerClients{render: make(map[string]RenderClient), casting: make(map[string]CastingClient)}
}

func (c *ownerClients) Render(binding Binding) RenderClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.render[binding.RenderURL]
}

func (c *ownerClients) Casting(binding Binding) CastingClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.casting[binding.CastingURL]
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

func (e *tenantEnv) addTenant(t *testing.T, owner app.Identity, renderClient *fakeRender, castingClient *fakeCasting) {
	t.Helper()
	key := ownerKey(owner)
	renderURL := "https://render-" + key + ".example"
	castingURL := "https://casting-" + key + ".example"
	e.bindings.byOwner[key] = Binding{RenderURL: renderURL, CastingURL: castingURL}
	e.clients.render[renderURL] = renderClient
	e.clients.casting[castingURL] = castingClient
}

func newFakeTenant(appID, instanceID, sessionID string) (*fakeRender, *fakeCasting) {
	instance := readyApp()
	instance.AppID = appID
	instance.InstanceID = instanceID
	return &fakeRender{instance: instance, getInstance: instance},
		&fakeCasting{target: testTarget(), session: casting.Session{ID: sessionID, State: casting.SessionPlaying}}
}

func idleAppFor(t *testing.T, id string, owner app.Identity) app.App {
	t.Helper()
	value := idleApp(t, owner)
	value.ID = id
	return value
}

func TestTenantsWithTheSameScreenIDUseTheirOwnServices(t *testing.T) {
	env := newTenantEnv(t)
	ownerA := app.Identity{DeviceID: "dev-a", AgentID: "agent-a", SessionID: "s-a"}
	ownerB := app.Identity{DeviceID: "dev-b", AgentID: "agent-b", SessionID: "s-b"}
	renderA, castingA := newFakeTenant("app_a", "inst_a", "ps_a")
	renderB, castingB := newFakeTenant("app_b", "inst_b", "ps_b")
	castingA.targets = []casting.Target{{ID: "tv_1", Name: "A-screen", State: casting.TargetOnline, Endpoints: testTarget().Endpoints}}
	castingB.targets = []casting.Target{{ID: "tv_1", Name: "B-screen", State: casting.TargetOnline, Endpoints: testTarget().Endpoints}}
	env.addTenant(t, ownerA, renderA, castingA)
	env.addTenant(t, ownerB, renderB, castingB)

	viewsA, err := env.svc.GetScreensFor(context.Background(), []app.Identity{ownerA})
	if err != nil || len(viewsA) != 1 || viewsA[0].Name != "A-screen" {
		t.Fatalf("A screens = %+v err=%v", viewsA, err)
	}
	viewsB, err := env.svc.GetScreensFor(context.Background(), []app.Identity{ownerB})
	if err != nil || len(viewsB) != 1 || viewsB[0].Name != "B-screen" {
		t.Fatalf("B screens = %+v err=%v", viewsB, err)
	}

	env.registry.Add(idleAppFor(t, "app_a", ownerA))
	env.registry.Add(idleAppFor(t, "app_b", ownerB))
	ctx := context.Background()
	if view, err := env.svc.startAppOnScreen(ctx, ownerA, "app_a", "tv_1", false); err != nil || view.State != app.StateRunning {
		t.Fatalf("start A: view=%+v err=%v", view, err)
	}
	if len(renderA.requests()) != 1 || len(castingA.requests()) != 1 {
		t.Fatalf("A services were not used: render=%d casting=%d", len(renderA.requests()), len(castingA.requests()))
	}
	if len(renderB.requests()) != 0 || len(castingB.requests()) != 0 {
		t.Fatal("starting A touched B's services")
	}

	if view, err := env.svc.startAppOnScreen(ctx, ownerB, "app_b", "tv_1", false); err != nil || view.State != app.StateRunning {
		t.Fatalf("start B: view=%+v err=%v", view, err)
	}
	if len(renderB.requests()) != 1 || len(castingB.requests()) != 1 {
		t.Fatal("B services were not used")
	}
	if len(renderA.requests()) != 1 || len(castingA.requests()) != 1 {
		t.Fatal("starting B touched A's services")
	}

	if _, err := env.svc.StopApp(ctx, ownerA, "app_a"); err != nil {
		t.Fatal(err)
	}
	if stopped := castingA.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_a" {
		t.Fatalf("A stopped = %v", stopped)
	}
	if stopped := castingB.stoppedIDs(); len(stopped) != 0 {
		t.Fatalf("B was stopped together with A: %v", stopped)
	}
	if deleted := renderA.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_a" {
		t.Fatalf("A deleted = %v", deleted)
	}
	if deleted := renderB.deletedIDs(); len(deleted) != 0 {
		t.Fatalf("B was deleted together with A: %v", deleted)
	}
}

func TestCrossOwnerForceIsRejectedOnASharedService(t *testing.T) {
	env := newTenantEnv(t)
	ownerA := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	ownerB := app.Identity{DeviceID: "dev-b", AgentID: "agent-b"}
	renderA, _ := newFakeTenant("app_a", "inst_a", "ps_a")
	renderB, castingB := newFakeTenant("app_b", "inst_b", "ps_b")
	env.bindings.byOwner[ownerKey(ownerA)] = Binding{RenderURL: "https://render-a.example", CastingURL: "https://casting.shared"}
	env.bindings.byOwner[ownerKey(ownerB)] = Binding{RenderURL: "https://render-b.example", CastingURL: "https://casting.shared"}
	env.clients.render["https://render-a.example"] = renderA
	env.clients.render["https://render-b.example"] = renderB
	env.clients.casting["https://casting.shared"] = castingB

	// B owns the session that the shared casting service reports.
	busy := testTarget()
	busy.ActiveSessionID = "ps_b"
	castingB.target = busy
	running := runningApp(ownerB)
	running.ID = "app_b"
	running.ScreenID = "tv_1"
	running.ProjectionSessionID = "ps_b"
	env.registry.Add(running)
	env.registry.Add(idleAppFor(t, "app_a", ownerA))

	_, err := env.svc.startAppOnScreen(context.Background(), ownerA, "app_a", "tv_1", true)
	if code := toolCode(t, err); code != "TARGET_BUSY" {
		t.Fatalf("code = %q", code)
	}
	if len(castingB.stoppedIDs()) != 0 {
		t.Fatal("force stopped another tenant's session")
	}
	if len(renderA.requests()) != 0 {
		t.Fatal("a Render instance was created for a screen that cannot be taken over")
	}
	if value, _ := env.registry.Get("app_b"); value.Snapshot().State != app.StateRunning {
		t.Fatal("the other tenant's app was changed")
	}
}

func TestForceStillReplacesTheSameOwnersApp(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	renderClient, castingClient := newFakeTenant("app_new", "inst_a", "ps_new")
	env.addTenant(t, owner, renderClient, castingClient)
	castingClient.target.ActiveSessionID = "ps_old"
	castingClient.session = casting.Session{ID: "ps_new", State: casting.SessionPlaying}

	previous := runningApp(owner)
	previous.ID = "app_old"
	previous.ProjectionSessionID = "ps_old"
	env.registry.Add(previous)
	env.registry.Add(idleAppFor(t, "app_new", owner))

	view, err := env.svc.startAppOnScreen(context.Background(), owner, "app_new", "tv_1", true)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != app.StateRunning || view.ReplacedAppID != "app_old" {
		t.Fatalf("view = %+v", view)
	}
	if stopped := castingClient.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_old" {
		t.Fatalf("stopped = %v", stopped)
	}
	if value, _ := env.registry.Get("app_old"); value.Snapshot().State != app.StateIdle {
		t.Fatalf("previous app = %+v", value.Snapshot())
	}
}

func TestOneTenantsSlowBindingDoesNotBlockAnother(t *testing.T) {
	env := newTenantEnv(t)
	ownerA := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	ownerB := app.Identity{DeviceID: "dev-b", AgentID: "agent-b"}
	renderA, castingA := newFakeTenant("app_a", "inst_a", "ps_a")
	renderB, castingB := newFakeTenant("app_b", "inst_b", "ps_b")
	env.addTenant(t, ownerA, renderA, castingA)
	env.addTenant(t, ownerB, renderB, castingB)
	env.registry.Add(idleAppFor(t, "app_a", ownerA))
	env.registry.Add(idleAppFor(t, "app_b", ownerB))

	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	env.bindings.mu.Lock()
	env.bindings.gates[ownerKey(ownerA)] = gate
	env.bindings.entered[ownerKey(ownerA)] = entered
	env.bindings.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := env.svc.startAppOnScreen(context.Background(), ownerA, "app_a", "tv_1", false)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("A never reached binding resolution")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := env.svc.startAppOnScreen(ctx, ownerB, "app_b", "tv_1", false); err != nil {
		t.Fatalf("B was blocked by A: %v", err)
	}

	close(gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("A after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A did not finish after its binding resolved")
	}
}

func TestActiveResourceGuardsAndStopAllForOwner(t *testing.T) {
	env := newTenantEnv(t)
	ownerA := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	ownerB := app.Identity{DeviceID: "dev-b", AgentID: "agent-b"}
	renderA, castingA := newFakeTenant("app_a", "inst_a", "ps_a")
	renderB, castingB := newFakeTenant("app_b", "inst_b", "ps_b")
	env.addTenant(t, ownerA, renderA, castingA)
	env.addTenant(t, ownerB, renderB, castingB)

	running := runningApp(ownerA)
	running.ID = "app_a"
	running.InstanceID = "inst_a"
	running.ProjectionSessionID = "ps_a"
	env.registry.Add(running)
	env.registry.Add(idleAppFor(t, "app_b", ownerB))

	if !env.svc.HasActiveRuntime([]app.Identity{ownerA}) {
		t.Fatal("A should have an active runtime")
	}
	if env.svc.HasActiveRuntime([]app.Identity{ownerB}) {
		t.Fatal("B has no active runtime")
	}
	if !env.svc.HasAnyActiveResources() {
		t.Fatal("the control plane reports no active resources")
	}

	failures := env.svc.StopAllForOwner(context.Background(), []app.Identity{ownerA})
	if len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
	if stopped := castingA.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_a" {
		t.Fatalf("stopped = %v", stopped)
	}
	if deleted := renderA.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_a" {
		t.Fatalf("deleted = %v", deleted)
	}
	if value, _ := env.registry.Get("app_a"); value.Snapshot().State != app.StateIdle || value.Snapshot().InstanceID != "" {
		t.Fatalf("app = %+v", value.Snapshot())
	}
	if env.svc.HasAnyActiveResources() {
		t.Fatal("resources were released but still reported active")
	}
	if len(castingB.stoppedIDs()) != 0 || len(renderB.deletedIDs()) != 0 {
		t.Fatal("stopping A touched B's services")
	}
}

func TestStopAllForOwnerReportsFailuresWithoutBlocking(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	renderClient, castingClient := newFakeTenant("app_a", "inst_a", "ps_a")
	castingClient.stopErr = errors.New("casting down")
	env.addTenant(t, owner, renderClient, castingClient)
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

func TestGetScreensForRequiresAnOwner(t *testing.T) {
	env := newTenantEnv(t)
	if _, err := env.svc.GetScreensFor(context.Background(), nil); toolCode(t, err) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("err = %v", err)
	}
}

func TestBindingFailureIsNotMasked(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	env.bindings.errs[ownerKey(owner)] = &ToolError{Code: "OWNER_NOT_FOUND", Message: "unknown"}
	env.registry.Add(idleAppFor(t, "app_a", owner))
	if _, err := env.svc.startAppOnScreen(context.Background(), owner, "app_a", "tv_1", false); toolCode(t, err) != "OWNER_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

// gatedRender blocks inside Create so a test can hold one owner inside a slow
// downstream Render call while another owner starts an app.
type gatedRender struct {
	*fakeRender
	entered chan struct{}
	gate    chan struct{}
}

func (g *gatedRender) Create(ctx context.Context, request render.CreateRequest) (render.App, error) {
	select {
	case g.entered <- struct{}{}:
	default:
	}
	select {
	case <-g.gate:
	case <-ctx.Done():
		return render.App{}, ctx.Err()
	}
	return g.fakeRender.Create(ctx, request)
}

func TestOneTenantsSlowRenderDoesNotBlockAnother(t *testing.T) {
	env := newTenantEnv(t)
	ownerA := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	ownerB := app.Identity{DeviceID: "dev-b", AgentID: "agent-b"}
	renderA, castingA := newFakeTenant("app_a", "inst_a", "ps_a")
	renderB, castingB := newFakeTenant("app_b", "inst_b", "ps_b")
	slow := &gatedRender{fakeRender: renderA, entered: make(chan struct{}, 1), gate: make(chan struct{})}

	// Both tenants use the same target ID on purpose: isolation must not depend
	// on downstream IDs being unique.
	renderAURL, castingAURL := "https://render-a.example", "https://casting-a.example"
	renderBURL, castingBURL := "https://render-b.example", "https://casting-b.example"
	env.bindings.byOwner[ownerKey(ownerA)] = Binding{RenderURL: renderAURL, CastingURL: castingAURL}
	env.bindings.byOwner[ownerKey(ownerB)] = Binding{RenderURL: renderBURL, CastingURL: castingBURL}
	env.clients.render[renderAURL] = slow
	env.clients.casting[castingAURL] = castingA
	env.clients.render[renderBURL] = renderB
	env.clients.casting[castingBURL] = castingB
	env.registry.Add(idleAppFor(t, "app_a", ownerA))
	env.registry.Add(idleAppFor(t, "app_b", ownerB))

	doneA := make(chan error, 1)
	go func() {
		_, err := env.svc.startAppOnScreen(context.Background(), ownerA, "app_a", "tv_1", false)
		doneA <- err
	}()
	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("A never reached Render")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := env.svc.startAppOnScreen(ctx, ownerB, "app_b", "tv_1", false); err != nil {
		t.Fatalf("B was blocked by A's slow Render: %v", err)
	}
	if requests := renderB.requests(); len(requests) != 1 {
		t.Fatalf("B Render calls = %d", len(requests))
	}
	if requests := renderA.requests(); len(requests) != 0 {
		t.Fatalf("A Render returned before it was released: %d calls", len(requests))
	}

	close(slow.gate)
	select {
	case err := <-doneA:
		if err != nil {
			t.Fatalf("A after its Render returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A did not finish after Render returned")
	}
	if requests := renderA.requests(); len(requests) != 1 {
		t.Fatalf("A Render calls = %d", len(requests))
	}
}

func TestSendRequestRejectsAPrivateAppBaseURL(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "dev-a", AgentID: "agent-a"}
	renderClient, castingClient := newFakeTenant("app_a", "inst_a", "ps_a")
	env.addTenant(t, owner, renderClient, castingClient)

	var hits atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	// Render reported a loopback address; forwarding to it would turn the
	// application operation into an SSRF probe of the deployment network.
	value := runningApp(owner)
	value.ID = "app_a"
	value.InstanceID = "inst_a"
	value.ProjectionSessionID = "ps_a"
	value.AppBaseURL = backend.URL
	value.OpenAPI = mustSpec(t)
	env.registry.Add(value)

	assertNoLeak := func(t *testing.T, err error, secrets ...string) {
		t.Helper()
		text := err.Error()
		for _, secret := range secrets {
			if secret != "" && strings.Contains(text, secret) {
				t.Fatalf("error leaked %q: %v", secret, err)
			}
		}
	}

	t.Run("netguard policy", func(t *testing.T) {
		env.svc.SetURLValidator(netguard.New(false).CheckURL)
		_, err := env.svc.SendRequest(context.Background(), owner, "app_a", "updateItem", nil, nil)
		if code := toolCode(t, err); code != netguard.CodePrivateTarget {
			t.Fatalf("code = %q, err = %v", code, err)
		}
		assertNoLeak(t, err, "127.0.0.1", backend.URL)
		if hits.Load() != 0 {
			t.Fatalf("the private address received %d requests", hits.Load())
		}
	})

	t.Run("validator without a stable code", func(t *testing.T) {
		env.svc.SetURLValidator(func(context.Context, string) error {
			return errors.New("blocked by policy")
		})
		_, err := env.svc.SendRequest(context.Background(), owner, "app_a", "updateItem", nil, nil)
		if code := toolCode(t, err); code != "INVALID_TARGET_URL" {
			t.Fatalf("code = %q, err = %v", code, err)
		}
		assertNoLeak(t, err, "127.0.0.1", backend.URL, "blocked by policy")
		if hits.Load() != 0 {
			t.Fatalf("the rejected address received %d requests", hits.Load())
		}
	})
}
