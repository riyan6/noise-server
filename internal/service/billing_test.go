package service

import (
	"noise-server/internal/model"
	"testing"
	"time"
)

func TestCalendarRenewalKeepsMonthEndAnchor(t *testing.T) {
	date := "2026-01-31"
	b := model.Billing{Cycle: "monthly", ExpiresAt: &date, AutoRenew: true}
	for _, pair := range [][2]string{{"2026-02-01", "2026-02-28"}, {"2026-03-01", "2026-03-31"}, {"2026-04-01", "2026-04-30"}, {"2026-05-01", "2026-05-31"}} {
		next := RenewedExpiry(b, 31, pair[0])
		if next == nil || *next != pair[1] {
			t.Fatalf("renewal at %s: %v", pair[0], next)
		}
		b.ExpiresAt = next
	}
	if RenewedExpiry(b, 31, "2026-05-31") != nil {
		t.Fatal("expiry date itself is still paid")
	}
	b.AutoRenew = false
	if RenewedExpiry(b, 31, "2027-01-01") != nil {
		t.Fatal("auto-renew off")
	}
}
func TestTrafficCalendarAndCounting(t *testing.T) {
	for _, item := range []struct {
		day         int
		today, want string
	}{{1, "2026-10-31", "2026-10-01"}, {15, "2026-01-03", "2025-12-15"}, {31, "2026-02-28", "2026-02-28"}, {31, "2026-03-05", "2026-02-28"}} {
		today, _ := time.Parse("2006-01-02", item.today)
		if got := PeriodStart(item.day, today); got != item.want {
			t.Fatalf("period: got %s, want %s", got, item.want)
		}
	}
	if trafficUsed("sum", 3, 5) != 8 || trafficUsed("max", 3, 5) != 5 || trafficUsed("min", 3, 5) != 3 {
		t.Fatal("wrong traffic method")
	}
}
func TestMonitorValidation(t *testing.T) {
	for _, value := range []struct {
		kind, target string
		valid        bool
	}{{"icmp", "1.1.1.1", true}, {"icmp", "2606:4700:4700::1111", true}, {"icmp", "example.com", true}, {"icmp", "1.1.1.1:80", false}, {"icmp", "999.1.1.1", false}, {"tcp", "1.1.1.1:80", true}, {"tcp", "[::1]:65535", true}, {"tcp", "example.com:0", false}, {"tcp", "example.com:65536", false}, {"tcp", "https://example.com:443", false}, {"tcp", "::1:80", false}} {
		if target(value.kind, value.target) != value.valid {
			t.Fatalf("%s %s", value.kind, value.target)
		}
	}
}
