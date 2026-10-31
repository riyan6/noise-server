package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type GeoResult struct {
	IP          string   `json:"ip"`
	CountryCode *string  `json:"countryCode"`
	Region      *string  `json:"region"`
	City        *string  `json:"city"`
	ASN         *int64   `json:"asn"`
	ASOrg       *string  `json:"asOrg"`
	ISP         *string  `json:"isp"`
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	Timezone    *string  `json:"timezone"`
}
type GeoLookup struct {
	OK      bool       `json:"ok"`
	Result  *GeoResult `json:"result,omitempty"`
	Reason  string     `json:"reason,omitempty"`
	Message string     `json:"message,omitempty"`
	Raw     any        `json:"raw,omitempty"`
	URL     string     `json:"url,omitempty"`
}

func LookupGeoIP(ctx context.Context, client *http.Client, provider, ip, userAgent string) GeoLookup {
	if ip != "" {
		a, e := netip.ParseAddr(ip)
		if e != nil {
			return GeoLookup{Reason: "rejected", Message: "Invalid IP address"}
		}
		if !a.IsGlobalUnicast() || a.IsPrivate() {
			return GeoLookup{Reason: "reserved"}
		}
	}
	var endpoint string
	switch provider {
	case "ipinfo":
		endpoint = "https://ipinfo.io/json"
		if ip != "" {
			endpoint = "https://ipinfo.io/" + url.PathEscape(ip) + "/json"
		}
	case "ip-api":
		endpoint = "http://ip-api.com/json"
		if ip != "" {
			endpoint += "/" + url.PathEscape(ip)
		}
	case "ip.sb":
		endpoint = "https://api.ip.sb/geoip"
		if ip != "" {
			endpoint += "/" + url.PathEscape(ip)
		}
	default:
		return GeoLookup{Reason: "rejected", Message: "Unknown GeoIP provider"}
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return GeoLookup{Reason: "unreachable", URL: endpoint}
	}
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	response, e := client.Do(request)
	if e != nil {
		return GeoLookup{Reason: networkReason(e), URL: endpoint}
	}
	defer response.Body.Close()
	var raw map[string]any
	if e = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&raw); e != nil {
		return GeoLookup{Reason: "rejected", Message: fmt.Sprintf("HTTP %d: invalid JSON", response.StatusCode), URL: endpoint}
	}
	fail := func(reason, message string) GeoLookup {
		return GeoLookup{Reason: reason, Message: message, Raw: raw, URL: endpoint}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fail("rejected", fmt.Sprintf("HTTP %d", response.StatusCode))
	}
	r := GeoResult{}
	switch provider {
	case "ipinfo":
		if raw["bogon"] == true {
			return fail("reserved", "")
		}
		if raw["error"] != nil {
			return fail("rejected", "Provider rejected the lookup")
		}
		r.IP = str(raw, "ip")
		r.CountryCode = ptr(raw, "country")
		r.Region = ptr(raw, "region")
		r.City = ptr(raw, "city")
		r.Timezone = ptr(raw, "timezone")
		r.ASN, r.ASOrg = parseAS(str(raw, "org"))
		parts := strings.Split(str(raw, "loc"), ",")
		if len(parts) == 2 {
			r.Latitude = parseFloat(parts[0])
			r.Longitude = parseFloat(parts[1])
		}
	case "ip-api":
		if str(raw, "status") == "fail" {
			message := str(raw, "message")
			if strings.Contains(message, "private range") || strings.Contains(message, "reserved range") {
				return fail("reserved", "")
			}
			return fail("rejected", message)
		}
		r.IP = str(raw, "query")
		r.CountryCode = ptr(raw, "countryCode")
		r.Region = ptr(raw, "regionName")
		r.City = ptr(raw, "city")
		r.Timezone = ptr(raw, "timezone")
		r.ISP = ptr(raw, "isp")
		r.ASN, r.ASOrg = parseAS(str(raw, "as"))
		r.Latitude = num(raw, "lat")
		r.Longitude = num(raw, "lon")
	case "ip.sb":
		r.IP = str(raw, "ip")
		r.CountryCode = ptr(raw, "country_code")
		r.Region = ptr(raw, "region")
		r.City = ptr(raw, "city")
		r.Timezone = ptr(raw, "timezone")
		r.ISP = ptr(raw, "isp")
		r.ASOrg = ptr(raw, "asn_organization")
		if v := num(raw, "asn"); v != nil {
			n := int64(*v)
			r.ASN = &n
		}
		r.Latitude = num(raw, "latitude")
		r.Longitude = num(raw, "longitude")
	}
	if _, e = netip.ParseAddr(r.IP); e != nil {
		return fail("rejected", "Provider did not return a valid IP address")
	}
	return GeoLookup{OK: true, Result: &r, Raw: raw, URL: endpoint}
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func ptr(m map[string]any, k string) *string {
	v := str(m, k)
	if v == "" {
		return nil
	}
	return &v
}
func num(m map[string]any, k string) *float64 {
	v, ok := m[k].(float64)
	if !ok {
		return nil
	}
	return &v
}
func parseFloat(v string) *float64 {
	n, e := strconv.ParseFloat(v, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func parseAS(v string) (*int64, *string) {
	if v == "" {
		return nil, nil
	}
	prefix, org, _ := strings.Cut(v, " ")
	n, e := strconv.ParseInt(strings.TrimPrefix(prefix, "AS"), 10, 64)
	if e != nil {
		return nil, &v
	}
	if org == "" {
		return &n, nil
	}
	return &n, &org
}
