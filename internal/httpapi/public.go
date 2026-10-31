package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"noise-server/internal/service"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *API) publicSettings(c *gin.Context) {
	s := a.service.Settings()
	success(c, gin.H{"siteName": s.SiteName, "defaultTheme": s.DefaultTheme, "defaultLang": s.DefaultLang, "refresh": s.Refresh})
}
func (a *API) publicNodes(c *gin.Context) {
	v, e := a.service.PublicNodes(c.Request.Context())
	reply(c, v, e)
}
func (a *API) publicNode(c *gin.Context) {
	v, e := a.service.PublicNode(c.Request.Context(), c.Param("id"))
	reply(c, v, e)
}
func (a *API) window(c *gin.Context) (service.Window, bool) {
	values := map[string]int64{}
	for _, key := range []string{"from", "to", "step"} {
		if raw := c.Query(key); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 0 {
				fail(c, 400, "invalid_request", key+" must be a non-negative integer")
				return service.Window{}, false
			}
			values[key] = v
		}
	}
	if values["step"] > int64(service.Retention/time.Second) {
		fail(c, 400, "invalid_request", "step exceeds seven days")
		return service.Window{}, false
	}
	w, e := a.service.Window(values["from"], values["to"], values["step"]*1000)
	if e != nil {
		respondError(c, e)
		return w, false
	}
	return w, true
}
func (a *API) history(c *gin.Context) {
	w, ok := a.window(c)
	if !ok {
		return
	}
	v, e := a.service.History(c.Request.Context(), c.Param("id"), w)
	reply(c, gin.H{"from": w.From, "to": w.To, "step": w.Step / 1000, "records": v}, e)
}
func (a *API) latency(c *gin.Context) {
	w, ok := a.window(c)
	if !ok {
		return
	}
	v, e := a.service.PublicLatency(c.Request.Context(), c.Param("id"), c.Query("taskId"), w)
	reply(c, gin.H{"from": w.From, "to": w.To, "step": float64(w.Step) / 1000, "series": v}, e)
}
func (a *API) events(c *gin.Context) {
	select {
	case a.streams <- struct{}{}:
		defer func() { <-a.streams }()
	default:
		fail(c, 503, "stream_limit", "Too many event streams")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("X-Accel-Buffering", "no")
	control := http.NewResponseController(c.Writer)
	send := func() bool {
		raw, e := a.snapshotData(c.Request.Context())
		if e != nil {
			return false
		}
		_ = control.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, e = fmt.Fprintf(c.Writer, "event: snapshot\ndata: %s\n\n", raw); e != nil {
			return false
		}
		return control.Flush() == nil
	}
	if !send() {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

// Fan out one serialized public snapshot instead of querying SQLite per subscriber.
func (a *API) snapshotData(ctx context.Context) ([]byte, error) {
	a.snapshotMu.Lock()
	defer a.snapshotMu.Unlock()
	now := time.Now()
	version := a.service.Version()
	if a.snapshot != nil && now.Sub(a.snapshotAt) < 500*time.Millisecond && version == a.snapshotVersion {
		return a.snapshot, nil
	}
	nodes, e := a.service.PublicNodes(ctx)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(gin.H{"nodes": nodes, "updatedAt": a.service.Now().UnixMilli()})
	if e != nil {
		return nil, e
	}
	a.snapshot = raw
	a.snapshotAt = now
	a.snapshotVersion = version
	return raw, nil
}
