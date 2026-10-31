package service

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"noise-server/internal/config"
	"noise-server/internal/model"
	"noise-server/internal/repository"
	"noise-server/internal/telemetry"

	"golang.org/x/crypto/bcrypt"
)

const Retention = 7 * 24 * time.Hour
const ProbeTimeout = 5
const SessionDuration = 7 * 24 * time.Hour

type Service struct {
	store      repository.Store
	cache      *telemetry.Cache
	settingsMu sync.RWMutex
	settings   model.Settings
	// Serialize read/modify/write operations, including ingestion and node deletion.
	mu       sync.Mutex
	revision atomic.Uint64
	Now      func() time.Time
	Location *time.Location
	Client   *http.Client
}

func New(ctx context.Context, store repository.Store, c config.Config) (*Service, error) {
	_, _, e := store.Admin(ctx)
	if errors.Is(e, model.ErrNotFound) {
		if !text(c.AdminUsername, 240, true) || len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 {
			return nil, invalid("first startup requires NOISE_ADMIN_PASSWORD (12–72 bytes) and a valid administrator username")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), bcrypt.DefaultCost+2)
		if err != nil {
			return nil, err
		}
		if err = store.CreateAdmin(ctx, c.AdminUsername, hash); err != nil {
			return nil, err
		}
	} else if e != nil {
		return nil, e
	}
	settings, e := store.Settings(ctx)
	if errors.Is(e, model.ErrNotFound) {
		settings = model.Settings{SiteName: "Noise", DefaultTheme: "light", DefaultLang: "zh", Refresh: 1, GeoIP: model.GeoIPConfig{Enabled: true, Provider: "ipinfo", UserAgent: "Noise/1.0"}, Agent: model.AgentSettings{Endpoint: c.APIURL, DashboardURL: c.DashboardURL}}
		if e = validateSettings(settings); e != nil {
			return nil, e
		}
		e = store.SaveSettings(ctx, settings)
	}
	if e != nil {
		return nil, e
	}
	runtimes, e := store.Runtimes(ctx)
	if e != nil {
		return nil, e
	}
	location := c.Location
	if location == nil {
		location = time.UTC
	}
	s := &Service{store: store, cache: telemetry.New(runtimes), settings: settings, Now: time.Now, Location: location, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	s.revision.Store(uint64(time.Now().UnixNano()))
	return s, nil
}
func (s *Service) Settings() model.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings
}
func (s *Service) SaveSettings(ctx context.Context, v model.Settings) error {
	if e := validateSettings(v); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.store.SaveSettings(ctx, v); e != nil {
		return e
	}
	s.settingsMu.Lock()
	s.settings = v
	s.settingsMu.Unlock()
	s.revision.Add(1)
	return nil
}
func (s *Service) Version() uint64 { return s.revision.Load() }
func (s *Service) online(runtime model.Runtime) bool {
	return runtime.ReceivedAt > 0 && s.Now().UnixMilli()-runtime.ReceivedAt < int64(max(10, s.Settings().Refresh*3))*1000
}
func (s *Service) Nodes(ctx context.Context) ([]model.AdminNode, error) {
	nodes, e := s.store.Nodes(ctx)
	if e != nil {
		return nil, e
	}
	result := make([]model.AdminNode, 0, len(nodes))
	for _, n := range nodes {
		value, e := s.decorate(ctx, n)
		if e != nil {
			return nil, e
		}
		result = append(result, value)
	}
	return result, nil
}
func (s *Service) decorate(ctx context.Context, n model.Node) (model.AdminNode, error) {
	value := model.AdminNode{Node: n, LatencyTaskIDs: []string{}}
	if r, ok := s.cache.Get(n.ID); ok {
		value.Online = s.online(r)
		value.LastSeen = &r.ReceivedAt
	}
	tasks, e := s.store.NodeTasks(ctx, n.ID)
	if e != nil {
		return value, e
	}
	for _, task := range tasks {
		value.LatencyTaskIDs = append(value.LatencyTaskIDs, task.ID)
	}
	return value, nil
}
func (s *Service) Node(ctx context.Context, id string) (model.AdminNode, error) {
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return model.AdminNode{}, e
	}
	return s.decorate(ctx, n)
}
func (s *Service) CreateNode(ctx context.Context, name, brand string) (model.AdminNode, error) {
	name = strings.TrimSpace(name)
	brand = strings.TrimSpace(brand)
	if !text(name, 240, true) || !text(brand, 240, true) {
		return model.AdminNode{}, invalid("node name and brand are required (maximum 240 bytes)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := model.Node{ID: rand.Text(), Token: rand.Text() + rand.Text(), Name: name, Brand: brand, CreatedAt: s.Now().UnixMilli(), Billing: model.Billing{Cycle: "monthly", Currency: "USD"}, Traffic: model.TrafficLimit{Method: "sum", Unit: "GB", ResetDay: 1}}
	if e := s.store.SaveNode(ctx, n); e != nil {
		return model.AdminNode{}, e
	}
	return s.decorate(ctx, n)
}

type NodeChanges struct {
	Name  *string `json:"name"`
	Brand *string `json:"brand"`
	Group *string `json:"group"`
	Note  *string `json:"note"`
}

func (s *Service) UpdateNode(ctx context.Context, id string, p NodeChanges) (model.AdminNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return model.AdminNode{}, e
	}
	if p.Name != nil {
		n.Name = strings.TrimSpace(*p.Name)
	}
	if p.Brand != nil {
		n.Brand = strings.TrimSpace(*p.Brand)
	}
	if p.Group != nil {
		n.Group = strings.TrimSpace(*p.Group)
	}
	if p.Note != nil {
		n.Note = strings.TrimSpace(*p.Note)
	}
	if !text(n.Name, 240, true) || !text(n.Brand, 240, true) || !text(n.Group, 240, false) || len(n.Note) > 4096 {
		return model.AdminNode{}, invalid("invalid node information")
	}
	if e = s.store.SaveNode(ctx, n); e != nil {
		return model.AdminNode{}, e
	}
	return s.decorate(ctx, n)
}
func (s *Service) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.store.DeleteNode(ctx, id); e != nil {
		return e
	}
	s.cache.Delete(id)
	s.revision.Add(1)
	return nil
}
func (s *Service) Labels(ctx context.Context, kind string) ([]string, error) {
	return s.store.Labels(ctx, kind)
}
func (s *Service) Billing(ctx context.Context, id string, b model.Billing) error {
	if e := validateBilling(b); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return e
	}
	if b.ExpiresAt == nil {
		n.BillingAnchor = 0
	} else if n.Billing.ExpiresAt == nil || *b.ExpiresAt != *n.Billing.ExpiresAt {
		d, _ := time.Parse("2006-01-02", *b.ExpiresAt)
		n.BillingAnchor = d.Day()
	}
	n.Billing = b
	if r, ok := s.cache.Get(id); ok && s.online(r) {
		s.renew(&n)
	}
	return s.store.SaveNode(ctx, n)
}
func (s *Service) Traffic(ctx context.Context, id string, t model.TrafficLimit) error {
	if e := validateTraffic(t); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return e
	}
	n.Traffic = t
	if e = s.store.SaveNode(ctx, n); e == nil {
		s.revision.Add(1)
	}
	return e
}
func (s *Service) renew(n *model.Node) bool {
	next := RenewedExpiry(n.Billing, n.BillingAnchor, s.Now().In(s.Location).Format("2006-01-02"))
	if next == nil {
		return false
	}
	n.Billing.ExpiresAt = next
	return true
}
func (s *Service) Tasks(ctx context.Context) ([]model.LatencyTask, error) { return s.store.Tasks(ctx) }

type TaskChanges struct {
	Name     *string   `json:"name"`
	Type     *string   `json:"type"`
	Target   *string   `json:"target"`
	Interval *int      `json:"interval"`
	NodeIDs  *[]string `json:"nodeIds"`
}

func (s *Service) SaveTask(ctx context.Context, id string, p TaskChanges) (model.LatencyTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UnixMilli()
	t := model.LatencyTask{ID: rand.Text(), Type: "icmp", Interval: 60, NodeIDs: []string{}, CreatedAt: now}
	if id != "" {
		tasks, e := s.store.Tasks(ctx)
		if e != nil {
			return t, e
		}
		found := false
		for _, known := range tasks {
			if known.ID == id {
				t = known
				found = true
				break
			}
		}
		if !found {
			return t, model.ErrNotFound
		}
	}
	if p.Name != nil {
		t.Name = strings.TrimSpace(*p.Name)
	}
	if p.Type != nil {
		t.Type = *p.Type
	}
	if p.Target != nil {
		t.Target = strings.TrimSpace(*p.Target)
	}
	if p.Interval != nil {
		t.Interval = *p.Interval
	}
	if p.NodeIDs != nil {
		t.NodeIDs = *p.NodeIDs
	}
	t.UpdatedAt = now
	if e := validateTask(t); e != nil {
		return t, e
	}
	for _, node := range t.NodeIDs {
		if _, e := s.store.Node(ctx, node); e != nil {
			return t, e
		}
	}
	if e := s.store.SaveTask(ctx, t); e != nil {
		return t, e
	}
	s.revision.Add(1)
	return t, nil
}
func (s *Service) DeleteTask(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.store.DeleteTask(ctx, id); e != nil {
		return e
	}
	s.revision.Add(1)
	return nil
}
func (s *Service) Assign(ctx context.Context, node string, ids []string) error {
	if !unique(ids) {
		return invalid("taskIds must be a unique ordered array")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ids == nil {
		ids = []string{}
	}
	if e := s.store.AssignTasks(ctx, node, ids); e != nil {
		return e
	}
	s.revision.Add(1)
	return nil
}
func (s *Service) AgentNode(ctx context.Context, token string) (model.Node, error) {
	if len(token) < 32 || len(token) > 128 {
		return model.Node{}, model.ErrUnauthorized
	}
	n, e := s.store.NodeByToken(ctx, token)
	if errors.Is(e, model.ErrNotFound) {
		e = model.ErrUnauthorized
	}
	return n, e
}
func (s *Service) UpdateInfo(ctx context.Context, id string, i model.AgentInfo) error {
	if e := validateInfo(i); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return e
	}
	n.Agent = &i
	return s.store.SaveNode(ctx, n)
}

type AgentTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Target   string `json:"target"`
	Interval int    `json:"interval"`
	Timeout  int    `json:"timeout"`
}
type AgentConfig struct {
	Version  string             `json:"version"`
	Refresh  int                `json:"refresh"`
	Traffic  model.TrafficLimit `json:"traffic"`
	Timezone string             `json:"timezone"`
	GeoIP    model.GeoIPConfig  `json:"geoip"`
	Tasks    []AgentTask        `json:"tasks"`
}

func (s *Service) AgentConfig(ctx context.Context, id string) (AgentConfig, error) {
	n, e := s.store.Node(ctx, id)
	if e != nil {
		return AgentConfig{}, e
	}
	tasks, e := s.store.NodeTasks(ctx, id)
	if e != nil {
		return AgentConfig{}, e
	}
	settings := s.Settings()
	result := AgentConfig{Version: versionString(s.Version()), Refresh: settings.Refresh, Traffic: n.Traffic, Timezone: s.Location.String(), GeoIP: settings.GeoIP, Tasks: []AgentTask{}}
	for _, t := range tasks {
		result.Tasks = append(result.Tasks, AgentTask{ID: t.ID, Name: t.Name, Type: t.Type, Target: t.Target, Interval: t.Interval, Timeout: ProbeTimeout})
	}
	return result, nil
}
