package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"noise-server/internal/model"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestTelegramEncodingAndFailureModes(t *testing.T) {
	topic := int64(123)
	config := model.TelegramConfig{BotToken: "123456:test_only_token", ChatID: "-100123456789", Endpoint: "https://api.telegram.org/bot", MessageThreadID: &topic}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot123456:test_only_token/sendMessage" {
			t.Fatalf("unexpected test request: %s", r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(b))
		if values.Get("chat_id") != config.ChatID || values.Get("message_thread_id") != "123" || values.Get("text") != "测试 & message" {
			t.Fatal("incorrect form encoding")
		}
		return response(`{"ok":true}`), nil
	})}
	if result := SendTelegram(context.Background(), client, config, "测试 & message"); !result.OK {
		t.Fatalf("send failed: %#v", result)
	}
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(`{"ok":false,"description":"chat not found"}`), nil
	})
	if r := SendTelegram(context.Background(), client, config, "test"); r.Reason != "rejected" || r.Description != "chat not found" {
		t.Fatal(r)
	}
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	if r := SendTelegram(context.Background(), client, config, "test"); r.Reason != "timeout" {
		t.Fatal(r)
	}
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if r := SendTelegram(context.Background(), client, config, "test"); r.Reason != "unreachable" {
		t.Fatal(r)
	}
}
func TestGeoIPProviderNormalization(t *testing.T) {
	tests := []struct{ provider, body string }{{"ipinfo", `{"ip":"1.1.1.1","country":"AU","city":"Sydney","org":"AS13335 Cloudflare","loc":"-33.86,151.20"}`}, {"ip-api", `{"status":"success","query":"1.1.1.1","countryCode":"AU","city":"Sydney","as":"AS13335 Cloudflare","lat":-33.86,"lon":151.20}`}, {"ip.sb", `{"ip":"1.1.1.1","country_code":"AU","city":"Sydney","asn":13335,"asn_organization":"Cloudflare","latitude":-33.86,"longitude":151.20}`}}
	for _, item := range tests {
		t.Run(item.provider, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("User-Agent") != "Noise-Test" {
					t.Fatal("missing configured user agent")
				}
				return response(item.body), nil
			})}
			r := LookupGeoIP(context.Background(), client, item.provider, "1.1.1.1", "Noise-Test")
			if !r.OK || r.Result.IP != "1.1.1.1" || *r.Result.CountryCode != "AU" || *r.Result.ASN != 13335 || *r.Result.ASOrg != "Cloudflare" || r.Result.Latitude == nil {
				t.Fatalf("bad normalized result: %#v", r)
			}
		})
	}
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("reserved addresses must not be sent to a provider")
		return nil, nil
	})}
	if r := LookupGeoIP(context.Background(), client, "ipinfo", "192.168.1.1", ""); r.Reason != "reserved" {
		t.Fatal(r)
	}
	if parseFloat("NaN") != nil || parseFloat("+Inf") != nil {
		t.Fatal("non-finite provider coordinates must be null")
	}
}
