package service

import (
	"fmt"
	"noise-server/internal/model"
	"time"
)

var cycleMonths = map[string]int{"monthly": 1, "quarterly": 3, "semiannual": 6, "annual": 12, "biennial": 24, "triennial": 36}

func monthDay(year int, month time.Month, day int) string {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}
func PeriodStart(day int, now time.Time) string {
	y, m, d := now.Date()
	start := monthDay(y, m, day)
	date, _ := time.Parse("2006-01-02", start)
	if d < date.Day() {
		previous := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		start = monthDay(previous.Year(), previous.Month(), day)
	}
	return start
}
func RenewedExpiry(b model.Billing, anchor int, today string) *string {
	if !b.AutoRenew || b.ExpiresAt == nil || *b.ExpiresAt >= today {
		return nil
	}
	start, e := time.Parse("2006-01-02", *b.ExpiresAt)
	if e != nil {
		return nil
	}
	months, ok := cycleMonths[b.Cycle]
	if !ok {
		return nil
	}
	if anchor < 1 || anchor > 31 {
		anchor = start.Day()
	}
	for n := 1; ; n++ {
		base := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, n*months, 0)
		next := monthDay(base.Year(), base.Month(), anchor)
		if next >= today {
			return &next
		}
	}
}

func trafficUsed(method string, up, down int64) int64 {
	if method == "max" {
		return max(up, down)
	}
	if method == "min" {
		return min(up, down)
	}
	return up + down
}
