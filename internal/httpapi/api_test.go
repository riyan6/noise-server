package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noise-server/internal/config"
	"noise-server/internal/httpapi"
	"noise-server/internal/model"
	"noise-server/internal/repository/sqlite"
	"noise-server/internal/service"
)

const adminOrigin = "https://admin.example.test"
const publicOrigin = "https://status.example.test"
const password = "test-only-password-12345"

type fixture struct {
	t       *testing.T
	handler http.Handler
	service *service.Service
	store   *sqlite.Store
	config  config.Config
	path    string
	clock   atomic.Int64
	cookie  *http.Cookie
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database with spaces & symbols.db")
	store, e := sqlite.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	location, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	c := config.Config{DatabasePath: path, AdminUsername: "admin", AdminPassword: password, AdminOrigins: []string{adminOrigin}, PublicOrigins: []string{publicOrigin}, CookieSecure: true, Location: location, APIURL: "https://api.example.test", DashboardURL: publicOrigin}
	s, e := service.New(context.Background(), store, c)
	if e != nil {
		store.Close()
		t.Fatal(e)
	}
	f := &fixture{t: t, service: s, store: store, config: c, path: path}
	f.clock.Store(time.Date(2026, 1, 31, 12, 0, 0, 0, location).UnixMilli())
	s.Now = func() time.Time { return time.UnixMilli(f.clock.Load()) }
	f.router()
	t.Cleanup(func() { f.store.Close() })
	return f
}
func (f *fixture) router() {
	r, e := httpapi.New(f.service, f.config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		f.t.Fatal(e)
	}
	f.handler = r
}
func (f *fixture) request(method, path string, body any, token, origin string, auth bool) *httptest.ResponseRecorder {
	f.t.Helper()
	var data []byte
	if body != nil {
		var e error
		data, e = json.Marshal(body)
		if e != nil {
			f.t.Fatal(e)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if auth && f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	result := httptest.NewRecorder()
	f.handler.ServeHTTP(result, req)
	return result
}
func decode[T any](t *testing.T, r *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if r.Code != status {
		t.Fatalf("HTTP %d, wanted %d: %s", r.Code, status, r.Body.String())
	}
	var envelope struct {
		Status string `json:"status"`
		Data   T      `json:"data"`
	}
	if status == 200 {
		if e := json.Unmarshal(r.Body.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		if envelope.Status != "success" {
			t.Fatalf("unexpected response: %s", r.Body.String())
		}
	}
	return envelope.Data
}
func (f *fixture) admin(method, path string, body any) *httptest.ResponseRecorder {
	return f.request(method, "/api/v1/admin"+path, body, "", adminOrigin, true)
}
func (f *fixture) login() {
	f.t.Helper()
	r := f.request("POST", "/api/v1/admin/auth/login", map[string]any{"username": "admin", "password": password}, "", adminOrigin, false)
	decode[map[string]any](f.t, r, 200)
	cookies := r.Result().Cookies()
	if len(cookies) != 1 {
		f.t.Fatal("missing session cookie")
	}
	f.cookie = cookies[0]
	if !f.cookie.Secure || !f.cookie.HttpOnly || f.cookie.SameSite != http.SameSiteLaxMode || f.cookie.Domain != "" {
		f.t.Fatalf("unsafe cookie configuration: %#v", f.cookie)
	}
}
func (f *fixture) node(name string) model.AdminNode {
	f.t.Helper()
	return decode[model.AdminNode](f.t, f.admin("POST", "/nodes", map[string]any{"name": name, "brand": "Example"}), 200)
}
func (f *fixture) token(id string) string {
	f.t.Helper()
	settings := f.service.Settings()
	settings.Agent.ScriptURL = "https://example.test/install.sh?x=1&y=2"
	if e := f.service.SaveSettings(context.Background(), settings); e != nil {
		f.t.Fatal(e)
	}
	v := decode[service.Installation](f.t, f.admin("GET", "/nodes/"+id+"/install", nil), 200)
	if !strings.Contains(v.Command, "'https://example.test/install.sh?x=1&y=2'") {
		f.t.Fatal("install command does not shell-quote URLs")
	}
	return v.Token
}
func (f *fixture) task(name string, nodes []string) model.LatencyTask {
	f.t.Helper()
	return decode[model.LatencyTask](f.t, f.admin("POST", "/latency/tasks", map[string]any{"name": name, "type": "icmp", "target": "1.1.1.1", "nodeIds": nodes}), 200)
}
func (f *fixture) report(token string, sequence int64, boot string, up, down int64) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.request("POST", "/api/v1/agent/report", model.Report{BootID: boot, Sequence: sequence, CPU: 25, Mem: 30, Disk: 40, Load: 0.5, Up: 100, Down: 200, TotalUp: up, TotalDown: down, Uptime: 1000}, token, "", false)
}
func (f *fixture) probe(token, task, run, status string, value *float64) *httptest.ResponseRecorder {
	return f.request("POST", "/api/v1/agent/latency-results", map[string]any{"results": []model.LatencyResult{{TaskID: task, RunID: run, Status: status, Value: value, Time: f.clock.Load()}}}, token, "", false)
}
func pointer[T any](v T) *T { return &v }
func (f *fixture) publicNode(id string) model.PublicNode {
	return decode[model.PublicNode](f.t, f.request("GET", "/api/v1/public/nodes/"+id, nil, "", publicOrigin, false), 200)
}

func TestAPIAuthenticationPrivacyAndBindings(t *testing.T) {
	f := newFixture(t)
	decode[any](t, f.admin("GET", "/nodes", nil), 401)
	decode[any](t, f.request("POST", "/api/v1/admin/auth/login", map[string]any{"username": "admin", "password": password}, "", "https://evil.example.test", false), 403)
	decode[any](t, f.request("POST", "/api/v1/admin/auth/login", map[string]any{"username": "admin", "password": password}, "", "", false), 403)
	preflight := f.request("OPTIONS", "/api/v1/admin/nodes", nil, "", adminOrigin, false)
	if preflight.Code != 204 || preflight.Header().Get("Access-Control-Allow-Origin") != adminOrigin || preflight.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("invalid CORS preflight")
	}
	f.login()
	a := f.node("Tokyo")
	b := f.node("Hong Kong")
	ta, tb := f.token(a.ID), f.token(b.ID)
	decode[any](t, f.admin("PATCH", "/nodes/"+a.ID, map[string]any{"note": "private-note-marker", "group": "core"}), 200)
	info := model.AgentInfo{IP: "192.0.2.123", IPv6: "2001:db8::123", Version: "secret-agent-version", Cores: 2, Memory: 4 << 30, Storage: 80 << 30, OS: "Debian 12", CPUModel: "Example CPU", Region: "JP", City: "Tokyo"}
	decode[any](t, f.request("POST", "/api/v1/agent/info", info, ta, "", false), 200)
	price := 6.25
	expiry := "2026-02-28"
	decode[any](t, f.admin("PUT", "/nodes/"+a.ID+"/billing", model.Billing{Cycle: "monthly", Currency: "USD", Price: &price, ExpiresAt: &expiry}), 200)
	first := f.task("First point", []string{a.ID})
	second := f.task("Second point", []string{a.ID})
	if first.Interval != 60 {
		t.Fatal("missing default interval")
	}
	decode[any](t, f.admin("PATCH", "/latency/tasks/"+first.ID, map[string]any{"interval": 0}), 400)
	decode[any](t, f.admin("PATCH", "/latency/tasks/"+first.ID, map[string]any{"type": "tcp", "target": "1.1.1.1"}), 400)
	decode[any](t, f.admin("PATCH", "/latency/tasks/"+second.ID, map[string]any{"type": "tcp", "target": "[2606:4700:4700::1111]:443", "interval": 120, "nodeIds": []string{a.ID, b.ID}}), 200)
	config := decode[service.AgentConfig](t, f.request("GET", "/api/v1/agent/config", nil, ta, "", false), 200)
	if len(config.Tasks) != 2 || config.Tasks[0].ID != first.ID || config.Tasks[1].Timeout != 5 || config.Tasks[1].Interval != 120 {
		t.Fatalf("invalid agent configuration: %#v", config)
	}
	decode[any](t, f.report(ta, 0, "boot-a", 100, 200), 200)
	decode[any](t, f.probe(ta, first.ID, "a1", "ok", pointer(80.0)), 200)
	decode[any](t, f.probe(ta, second.ID, "b1", "ok", pointer(10.0)), 200)
	if n := f.publicNode(a.ID); n.RTT == nil || *n.RTT != 80 || n.Billing.Price == nil || *n.Billing.Price != price || *n.Billing.ExpiresAt != expiry {
		t.Fatalf("invalid public node: %#v", n)
	}
	decode[any](t, f.probe(tb, first.ID, "unauthorized-task", "ok", pointer(1.0)), 400)
	decode[any](t, f.admin("PUT", "/nodes/"+a.ID+"/latency-tasks", map[string]any{"taskIds": []string{second.ID, first.ID}}), 200)
	decode[any](t, f.admin("PUT", "/nodes/"+a.ID+"/latency-tasks", map[string]any{"taskIds": []string{first.ID, "missing-task"}}), 409)
	if n := f.publicNode(a.ID); n.RTT == nil || *n.RTT != 10 {
		t.Fatal("task assignment transaction/order is incorrect")
	}
	f.clock.Add(1000)
	decode[any](t, f.probe(ta, second.ID, "b2", "timeout", nil), 200)
	if f.publicNode(a.ID).RTT != nil {
		t.Fatal("timeout must be null, without falling back to another task")
	}
	endpoints := []string{"/api/v1/public/nodes", "/api/v1/public/nodes/" + a.ID, "/api/v1/public/nodes/" + a.ID + "/history", "/api/v1/public/nodes/" + a.ID + "/latency", "/api/v1/public/settings"}
	for _, endpoint := range endpoints {
		r := f.request("GET", endpoint, nil, "", publicOrigin, false)
		decode[any](t, r, 200)
		for _, secret := range []string{info.IP, info.IPv6, info.Version, "private-note-marker", ta, tb, "install.sh"} {
			if strings.Contains(r.Body.String(), secret) {
				t.Fatalf("public data leak at %s: %s", endpoint, secret)
			}
		}
	}
	node := decode[model.AdminNode](t, f.admin("GET", "/nodes/"+a.ID, nil), 200)
	if node.Agent.IP != info.IP || node.Note != "private-note-marker" || node.LatencyTaskIDs[0] != second.ID {
		t.Fatal("admin metadata or binding order missing")
	}
	decode[any](t, f.admin("DELETE", "/latency/tasks/"+second.ID, nil), 200)
	n := f.publicNode(a.ID)
	if len(n.Tasks) != 1 || n.Tasks[0].ID != first.ID || n.RTT == nil || *n.RTT != 80 {
		t.Fatal("task deletion did not preserve the next binding")
	}
	decode[any](t, f.admin("DELETE", "/nodes/"+b.ID, nil), 200)
	decode[any](t, f.report(tb, 1, "boot-b", 0, 0), 401)
	f.clock.Add(11_000)
	if n = f.publicNode(a.ID); n.Online || n.CPU != nil || n.RTT != nil {
		t.Fatal("offline metrics must be null")
	}
	decode[any](t, f.admin("POST", "/auth/logout", nil), 200)
	decode[any](t, f.admin("GET", "/nodes", nil), 401)
}

func TestReportPersistenceIdempotencyTrafficAndRetention(t *testing.T) {
	f := newFixture(t)
	f.login()
	node := f.node("Persistent")
	token := f.token(node.ID)
	task := f.task("Ping", []string{node.ID})
	decode[any](t, f.report(token, 0, "boot-1", 100, 200), 200)
	f.clock.Add(1000)
	decode[any](t, f.report(token, 1, "boot-1", 150, 280), 200)
	received := f.publicNode(node.ID).LastSeen
	f.clock.Add(1000)
	decode[any](t, f.report(token, 1, "boot-1", 999, 999), 200)
	n := f.publicNode(node.ID)
	if n.PeriodUp != 50 || n.PeriodDown != 80 || n.Total != 130 || *n.LastSeen != *received {
		t.Fatalf("duplicate report counted twice: %#v", n)
	}
	decode[any](t, f.report(token, 0, "boot-1", 1, 1), 409)
	decode[any](t, f.report(token, 0, "boot-2", 20, 30), 200)
	decode[any](t, f.report(token, 2, "boot-1", 200, 300), 409)
	n = f.publicNode(node.ID)
	if n.PeriodUsed != 180 {
		t.Fatalf("counter reset accounting: %d", n.PeriodUsed)
	}
	decode[any](t, f.admin("PUT", "/nodes/"+node.ID+"/traffic", model.TrafficLimit{Method: "max", Unit: "GB", ResetDay: 1}), 200)
	if f.publicNode(node.ID).PeriodUsed != 110 {
		t.Fatal("max traffic calculation failed")
	}
	decode[any](t, f.probe(token, task.ID, "once", "ok", pointer(20.0)), 200)
	decode[any](t, f.probe(token, task.ID, "once", "ok", pointer(900.0)), 200)
	if n = f.publicNode(node.ID); n.RTT == nil || *n.RTT != 20 {
		t.Fatal("duplicate probe replaced original data")
	}
	before := f.clock.Load()
	if e := f.store.Close(); e != nil {
		t.Fatal(e)
	}
	store, e := sqlite.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	f.store = store
	c := f.config
	c.AdminPassword = ""
	s, e := service.New(context.Background(), store, c)
	if e != nil {
		t.Fatal(e)
	}
	f.service = s
	s.Now = func() time.Time { return time.UnixMilli(f.clock.Load()) }
	f.router()
	if n = f.publicNode(node.ID); n.Total != 180 || n.PeriodUsed != 110 || !n.Online {
		t.Fatal("runtime or traffic did not survive restart")
	}
	decode[any](t, f.admin("GET", "/auth/me", nil), 200)
	decode[any](t, f.report(token, 0, "boot-2", 20, 30), 200)
	if f.publicNode(node.ID).Total != 180 {
		t.Fatal("retry after restart was counted again")
	}
	settings := f.service.Settings()
	settings.Agent.DashboardURL = "https://new-status.example.test"
	decode[any](t, f.admin("PUT", "/settings", settings), 200)
	stored, e := f.store.Settings(context.Background())
	if e != nil || stored.Agent.DashboardURL != settings.Agent.DashboardURL {
		t.Fatal("settings were not persisted")
	}
	f.clock.Add(int64(8 * 24 * time.Hour / time.Millisecond))
	decode[any](t, f.report(token, 1, "boot-2", 30, 40), 200)
	if e = f.service.Maintain(context.Background()); e != nil {
		t.Fatal(e)
	}
	history, e := f.store.History(context.Background(), node.ID, 0, before, 1000)
	if e != nil || len(history) != 0 {
		t.Fatalf("old metrics retained: %v %#v", e, history)
	}
	probe, e := f.store.LatestLatency(context.Background(), node.ID, task.ID)
	if e != nil || probe != nil {
		t.Fatal("old latency probes retained")
	}
	decode[any](t, f.admin("GET", "/auth/me", nil), 401)
	w, e := f.service.Window(1, 0, 0)
	if e != nil || w.From < f.clock.Load()-int64(service.Retention/time.Millisecond) {
		t.Fatal("history query exceeds retention window")
	}
}

func TestSSEInitialSnapshotAndDisconnect(t *testing.T) {
	f := newFixture(t)
	f.login()
	node := f.node("SSE node")
	token := f.token(node.ID)
	decode[any](t, f.report(token, 0, "boot", 0, 0), 200)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/public/events", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", publicOrigin)
	client := &http.Client{Timeout: 5 * time.Second}
	response, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("not an event stream")
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() || scanner.Text() != "event: snapshot" {
		t.Fatal("missing snapshot event")
	}
	if !scanner.Scan() || !strings.Contains(scanner.Text(), "SSE node") || strings.Contains(scanner.Text(), token) {
		t.Fatal("invalid SSE payload")
	}
	cancel()
}

func TestLoginRateLimitAndBodyValidation(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 10; i++ {
		decode[any](t, f.request("POST", "/api/v1/admin/auth/login", map[string]any{"username": "admin", "password": "wrong"}, "", adminOrigin, false), 401)
	}
	decode[any](t, f.request("POST", "/api/v1/admin/auth/login", map[string]any{"username": "admin", "password": password}, "", adminOrigin, false), 429)
	f.router()
	f.login()
	decode[any](t, f.admin("POST", "/nodes", map[string]any{"name": "a", "brand": "b", "token": "must-not-be-client-controlled"}), 400)
	decode[any](t, f.admin("POST", "/nodes", map[string]any{"name": strings.Repeat("a", 1<<20), "brand": "b"}), 413)
}

func TestChannelsKeepSecretsAndUseBackendTransport(t *testing.T) {
	f := newFixture(t)
	f.login()
	token := "123456:test_only_secret_value"
	config := model.TelegramConfig{BotToken: token, ChatID: "-100123456789", Endpoint: "https://api.telegram.org/bot"}
	channel := decode[model.Channel](t, f.admin("POST", "/channels", map[string]any{"type": "telegram", "name": "Ops", "config": config}), 200)
	if channel.Config.BotToken != "" || !strings.HasSuffix(channel.TokenHint, "alue") {
		t.Fatal("channel token was not masked")
	}
	list := f.admin("GET", "/channels", nil)
	decode[any](t, list, 200)
	if strings.Contains(list.Body.String(), token) {
		t.Fatal("channel list leaked a bot token")
	}
	config.BotToken = ""
	decode[model.Channel](t, f.admin("PATCH", "/channels/"+channel.ID, map[string]any{"config": config}), 200)
	stored, e := f.store.Channels(context.Background())
	if e != nil || stored[0].Config.BotToken != token {
		t.Fatal("blank edit erased saved token")
	}
	calls := 0
	f.service.Client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.Contains(r.URL.Path, token) {
			t.Fatal("test did not use saved token")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	result := decode[map[string]any](t, f.admin("POST", "/channels/test", map[string]any{"channelId": channel.ID, "config": config}), 200)
	if result["ok"] != true || calls != 1 {
		t.Fatal("channel test failed")
	}
	decode[any](t, f.admin("DELETE", "/channels/"+channel.ID, nil), 200)
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConcurrentReportsConvergeAndRenewalKeepsAnchor(t *testing.T) {
	f := newFixture(t)
	f.login()
	node := f.node("Concurrent")
	token := f.token(node.ID)
	date := "2026-01-31"
	decode[any](t, f.admin("PUT", "/nodes/"+node.ID+"/billing", model.Billing{Cycle: "monthly", Currency: "USD", ExpiresAt: &date, AutoRenew: true}), 200)
	decode[any](t, f.report(token, 0, "boot", 100, 200), 200)
	var wait sync.WaitGroup
	results := make(chan int, 20)
	for i := int64(1); i <= 20; i++ {
		wait.Add(1)
		go func(seq int64) { defer wait.Done(); results <- f.report(token, seq, "boot", 100+seq, 200+seq).Code }(i)
	}
	wait.Wait()
	close(results)
	for code := range results {
		if code != 200 && code != 409 {
			t.Fatalf("concurrent report returned %d", code)
		}
	}
	if got := f.publicNode(node.ID).Total; got != 40 {
		t.Fatalf("concurrent accounting = %d", got)
	}
	f.clock.Store(time.Date(2026, 2, 1, 12, 0, 0, 0, f.config.Location).UnixMilli())
	decode[any](t, f.report(token, 21, "boot", 121, 221), 200)
	n := decode[model.AdminNode](t, f.admin("GET", "/nodes/"+node.ID, nil), 200)
	if *n.Billing.ExpiresAt != "2026-02-28" {
		t.Fatal("February renewal failed")
	}
	f.clock.Store(time.Date(2026, 3, 1, 12, 0, 0, 0, f.config.Location).UnixMilli())
	decode[any](t, f.report(token, 22, "boot", 122, 222), 200)
	// The existing session is deliberately expired after a month; use the service to inspect persisted state.
	n, e := f.service.Node(context.Background(), node.ID)
	if e != nil || *n.Billing.ExpiresAt != "2026-03-31" {
		t.Fatal("month-end billing anchor drifted")
	}
}

func TestOSRebootCountsNewCountersEvenWhenTheyAreLarger(t *testing.T) {
	f := newFixture(t)
	f.login()
	node := f.node("Reboot")
	token := f.token(node.ID)
	decode[any](t, f.report(token, 0, "old-process", 10, 20), 200)
	f.clock.Add(1000)
	report := model.Report{BootID: "new-process", Sequence: 0, Uptime: 5, CPU: 1, Mem: 2, Disk: 3, TotalUp: 100, TotalDown: 200}
	decode[any](t, f.request("POST", "/api/v1/agent/report", report, token, "", false), 200)
	if n := f.publicNode(node.ID); n.PeriodUp != 100 || n.PeriodDown != 200 || n.Total != 300 {
		t.Fatalf("reboot accounting lost traffic: %#v", n)
	}
}
