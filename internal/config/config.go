package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Config struct {
	Address, DatabasePath, AdminUsername, AdminPassword, APIURL, DashboardURL string
	AdminOrigins, PublicOrigins, TrustedProxies                               []string
	CookieSecure                                                              bool
	Location                                                                  *time.Location
}

func Load() (Config, error) {
	get := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	}
	secure, err := strconv.ParseBool(get("NOISE_COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("NOISE_COOKIE_SECURE: %w", err)
	}
	location, err := time.LoadLocation(get("NOISE_TIMEZONE", "Asia/Shanghai"))
	if err != nil {
		return Config{}, fmt.Errorf("NOISE_TIMEZONE: %w", err)
	}
	c := Config{Address: get("NOISE_ADDR", "127.0.0.1:8080"), DatabasePath: get("NOISE_DB", "data/noise.db"), AdminUsername: get("NOISE_ADMIN_USERNAME", "admin"), AdminPassword: os.Getenv("NOISE_ADMIN_PASSWORD"), APIURL: get("NOISE_API_URL", "http://localhost:8080"), DashboardURL: get("NOISE_DASHBOARD_URL", "http://localhost:5173"), AdminOrigins: split(get("NOISE_ADMIN_ORIGINS", "http://localhost:5174")), PublicOrigins: split(get("NOISE_PUBLIC_ORIGINS", "http://localhost:5173")), TrustedProxies: split(os.Getenv("NOISE_TRUSTED_PROXIES")), CookieSecure: secure, Location: location}
	for _, origin := range append(append([]string{}, c.AdminOrigins...), c.PublicOrigins...) {
		u, e := url.Parse(origin)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("invalid allowed origin: %q (use scheme://host[:port], without a path)", origin)
		}
	}
	return c, nil
}

func split(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
