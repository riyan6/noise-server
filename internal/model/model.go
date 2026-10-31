package model

import "errors"

var (
	ErrNotFound     = errors.New("resource not found")
	ErrConflict     = errors.New("resource conflict")
	ErrUnauthorized = errors.New("invalid credentials")
)

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type Billing struct {
	Cycle     string   `json:"cycle"`
	Price     *float64 `json:"price"`
	Currency  string   `json:"currency"`
	ExpiresAt *string  `json:"expiresAt"`
	AutoRenew bool     `json:"autoRenew"`
}
type TrafficLimit struct {
	Method   string `json:"method"`
	Bytes    *int64 `json:"bytes"`
	Unit     string `json:"unit"`
	ResetDay int    `json:"resetDay"`
}
type AgentInfo struct {
	IP       string `json:"ip"`
	IPv6     string `json:"ipv6,omitempty"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	CPUModel string `json:"cpuModel"`
	Cores    int    `json:"cores"`
	Memory   int64  `json:"memory"`
	Storage  int64  `json:"storage"`
	Region   string `json:"region"`
	City     string `json:"city"`
}
type Node struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Brand         string       `json:"brand"`
	Group         string       `json:"group"`
	Note          string       `json:"note"`
	Billing       Billing      `json:"billing"`
	Traffic       TrafficLimit `json:"traffic"`
	Agent         *AgentInfo   `json:"agent"`
	CreatedAt     int64        `json:"createdAt"`
	Token         string       `json:"-"`
	BillingAnchor int          `json:"-"`
}
type AdminNode struct {
	Node
	Online         bool     `json:"online"`
	LastSeen       *int64   `json:"lastSeen"`
	LatencyTaskIDs []string `json:"latencyTaskIds"`
}
type LatencyTask struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Target    string   `json:"target"`
	Interval  int      `json:"interval"`
	NodeIDs   []string `json:"nodeIds"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}
type TelegramConfig struct {
	BotToken        string `json:"botToken"`
	ChatID          string `json:"chatId"`
	Endpoint        string `json:"endpoint"`
	MessageThreadID *int64 `json:"messageThreadId"`
}
type Channel struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    TelegramConfig `json:"config"`
	TokenHint string         `json:"tokenHint,omitempty"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}
type GeoIPConfig struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider"`
	UserAgent string `json:"userAgent"`
}
type AgentSettings struct {
	ScriptURL    string `json:"scriptUrl"`
	Endpoint     string `json:"endpoint"`
	DashboardURL string `json:"dashboardUrl"`
}
type Settings struct {
	SiteName     string        `json:"siteName"`
	DefaultTheme string        `json:"defaultTheme"`
	DefaultLang  string        `json:"defaultLang"`
	Refresh      int           `json:"refresh"`
	GeoIP        GeoIPConfig   `json:"geoip"`
	Agent        AgentSettings `json:"agent"`
}

// CPU/memory/disk are percentages, rates are bytes/s, counters/capacities are bytes.
// BootID identifies an agent process instance; Sequence strictly increases within it.
type Report struct {
	BootID    string  `json:"bootId"`
	Sequence  int64   `json:"sequence"`
	Uptime    float64 `json:"uptime"`
	CPU       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	Disk      float64 `json:"disk"`
	Load      float64 `json:"load"`
	Up        float64 `json:"up"`
	Down      float64 `json:"down"`
	TotalUp   int64   `json:"totalUp"`
	TotalDown int64   `json:"totalDown"`
}
type Runtime struct {
	Report     Report `json:"report"`
	ReceivedAt int64  `json:"receivedAt"`
	TotalUp    int64  `json:"totalUp"`
	TotalDown  int64  `json:"totalDown"`
}
type LatencyResult struct {
	TaskID string   `json:"taskId"`
	RunID  string   `json:"runId"`
	Status string   `json:"status"`
	Value  *float64 `json:"value"`
	Time   int64    `json:"time"`
}
type Sample struct {
	Time int64    `json:"time"`
	CPU  *float64 `json:"cpu"`
	Mem  *float64 `json:"mem"`
	Up   *float64 `json:"up"`
	Down *float64 `json:"down"`
	RTT  *float64 `json:"rtt"`
}
type PublicTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Interval int    `json:"interval"`
}
type PublicBilling struct {
	Cycle     string   `json:"cycle"`
	Price     *float64 `json:"price"`
	Currency  string   `json:"currency"`
	ExpiresAt *string  `json:"expiresAt"`
}

// Explicit allowlist: never serialize Node or AgentInfo on a public route.
type PublicNode struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Provider    string        `json:"provider"`
	Group       string        `json:"group"`
	Region      string        `json:"region"`
	City        string        `json:"city"`
	OS          string        `json:"os"`
	Kernel      string        `json:"kernel"`
	Arch        string        `json:"arch"`
	CPUModel    string        `json:"cpuModel"`
	Cores       int           `json:"cores"`
	Memory      int64         `json:"memory"`
	Storage     int64         `json:"storage"`
	Billing     PublicBilling `json:"billing"`
	Traffic     TrafficLimit  `json:"traffic"`
	Online      bool          `json:"online"`
	LastSeen    *int64        `json:"lastSeen"`
	Uptime      float64       `json:"uptime"`
	CPU         *float64      `json:"cpu"`
	Mem         *float64      `json:"mem"`
	Disk        *float64      `json:"disk"`
	Up          *float64      `json:"up"`
	Down        *float64      `json:"down"`
	Load        *float64      `json:"load"`
	RTT         *float64      `json:"rtt"`
	Total       int64         `json:"total"`
	PeriodStart string        `json:"periodStart"`
	PeriodUp    int64         `json:"periodUp"`
	PeriodDown  int64         `json:"periodDown"`
	PeriodUsed  int64         `json:"periodUsed"`
	Tasks       []PublicTask  `json:"tasks"`
}
