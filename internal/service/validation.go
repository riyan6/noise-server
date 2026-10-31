package service

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"noise-server/internal/model"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func invalid(message string) error { return model.ValidationError(message) }
func text(value string, max int, required bool) bool {
	return utf8.ValidString(value) && len(value) <= max && (!required || strings.TrimSpace(value) != "") && !strings.ContainsAny(value, "\x00\r\n")
}
func URL(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(value) <= 2048
}
func contains(value string, values ...string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func unique(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !text(id, 128, true) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return len(ids) <= 1024
}

var labelPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
var digits = regexp.MustCompile(`^[0-9]+$`)

func host(value string) bool {
	if _, e := netip.ParseAddr(value); e == nil {
		return true
	}
	if len(value) == 0 || len(value) > 253 || strings.ContainsAny(value, ":/[] \t\r\n") {
		return false
	}
	numeric := true
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			numeric = false
			break
		}
	}
	if numeric {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(value, "."), ".") {
		if !labelPattern.MatchString(part) {
			return false
		}
	}
	return true
}
func target(kind, value string) bool {
	if kind == "icmp" {
		return host(value)
	}
	if kind != "tcp" {
		return false
	}
	h, p, e := net.SplitHostPort(value)
	if e != nil || !host(h) || !digits.MatchString(p) {
		return false
	}
	port, e := strconv.Atoi(p)
	return e == nil && port >= 1 && port <= 65535
}
func validateBilling(b model.Billing) error {
	if _, ok := cycleMonths[b.Cycle]; !ok {
		return invalid("invalid billing cycle")
	}
	if !contains(b.Currency, "CNY", "USD", "EUR", "HKD", "JPY") {
		return invalid("invalid currency")
	}
	if b.Price != nil && (math.IsNaN(*b.Price) || math.IsInf(*b.Price, 0) || *b.Price < 0 || *b.Price > 1e12) {
		return invalid("invalid price")
	}
	if b.ExpiresAt != nil {
		if _, e := time.Parse("2006-01-02", *b.ExpiresAt); e != nil {
			return invalid("expiry must be YYYY-MM-DD")
		}
	} else if b.AutoRenew {
		return invalid("auto-renew requires an expiry date")
	}
	return nil
}
func validateTraffic(t model.TrafficLimit) error {
	if !contains(t.Method, "sum", "max", "min") || !contains(t.Unit, "B", "KB", "MB", "GB", "TB", "PB") || t.ResetDay < 1 || t.ResetDay > 31 || (t.Bytes != nil && (*t.Bytes < 0 || *t.Bytes > 9007199254740991)) {
		return invalid("invalid traffic configuration")
	}
	return nil
}
func validateTask(t model.LatencyTask) error {
	if !text(t.Name, 240, true) || !target(t.Type, t.Target) || t.Interval < 1 || t.Interval > 86400 || !unique(t.NodeIDs) {
		return invalid("invalid monitor: name, ICMP/TCP target, interval (1–86400 seconds), or node IDs")
	}
	return nil
}
func validateSettings(s model.Settings) error {
	if !text(s.SiteName, 240, true) || !contains(s.DefaultTheme, "light", "dark") || !contains(s.DefaultLang, "zh", "en") || !contains(fmt.Sprint(s.Refresh), "1", "2", "5", "10") {
		return invalid("invalid site settings")
	}
	if !contains(s.GeoIP.Provider, "none", "ipinfo", "ip-api", "ip.sb") || !text(s.GeoIP.UserAgent, 512, false) {
		return invalid("invalid GeoIP settings")
	}
	if !URL(s.Agent.ScriptURL, true) || !URL(s.Agent.Endpoint, false) || !URL(s.Agent.DashboardURL, false) {
		return invalid("agent and dashboard addresses must be HTTP(S) URLs")
	}
	return nil
}
func validateInfo(i model.AgentInfo) error {
	if i.IP != "" {
		if _, e := netip.ParseAddr(i.IP); e != nil {
			return invalid("invalid IP address")
		}
	}
	if i.IPv6 != "" {
		a, e := netip.ParseAddr(i.IPv6)
		if e != nil || !a.Is6() {
			return invalid("invalid IPv6 address")
		}
	}
	for _, v := range []string{i.Version, i.OS, i.Kernel, i.Arch, i.CPUModel, i.Region, i.City} {
		if !text(v, 256, false) {
			return invalid("invalid agent information")
		}
	}
	if i.Cores < 1 || i.Cores > 1048576 || i.Memory < 1 || i.Storage < 0 {
		return invalid("invalid hardware capacity")
	}
	return nil
}
func validateReport(r model.Report) error {
	if !text(r.BootID, 128, true) || r.Sequence < 0 || r.TotalUp < 0 || r.TotalDown < 0 || r.TotalUp > 9007199254740991 || r.TotalDown > 9007199254740991 {
		return invalid("invalid report identity or counters")
	}
	for _, v := range []float64{r.Uptime, r.CPU, r.Mem, r.Disk, r.Load, r.Up, r.Down} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e15 {
			return invalid("invalid metric")
		}
	}
	if r.CPU > 100 || r.Mem > 100 || r.Disk > 100 {
		return invalid("resource percentages must be between 0 and 100")
	}
	return nil
}
