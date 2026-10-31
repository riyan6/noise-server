package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"noise-server/internal/config"
	"path/filepath"
	"testing"
	"time"
)

type startupLog struct{ address chan string }

func (l startupLog) Write(p []byte) (int, error) {
	var entry struct {
		Message string `json:"msg"`
		Address string `json:"address"`
	}
	if json.Unmarshal(p, &entry) == nil && entry.Message == "noise-server starting" {
		select {
		case l.address <- entry.Address:
		default:
		}
	}
	return len(p), nil
}
func TestRunHealthAndGracefulShutdown(t *testing.T) {
	logs := startupLog{address: make(chan string, 1)}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := config.Config{Address: "127.0.0.1:0", DatabasePath: filepath.Join(t.TempDir(), "app.db"), AdminUsername: "admin", AdminPassword: "test-only-password-12345", APIURL: "http://localhost:8080", DashboardURL: "http://localhost:5173", Location: time.UTC}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, logger) }()
	var address string
	select {
	case address = <-logs.address:
	case e := <-done:
		t.Fatalf("startup failed: %v", e)
	case <-time.After(15 * time.Second):
		t.Fatal("startup timeout")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	r, e := client.Get("http://" + address + "/health")
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("health returned %d", r.StatusCode)
	}
	stream, e := client.Get("http://" + address + "/api/v1/public/events")
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatalf("stream returned %d", stream.StatusCode)
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("graceful shutdown did not stop SSE and scheduler")
	}
}
