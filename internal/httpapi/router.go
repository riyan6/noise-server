package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"noise-server/internal/config"
	"noise-server/internal/model"
	"noise-server/internal/service"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const cookieName = "noise_session"

type API struct {
	service         *service.Service
	config          config.Config
	limiter         loginLimiter
	log             *slog.Logger
	streams         chan struct{}
	snapshotMu      sync.Mutex
	snapshotAt      time.Time
	snapshotVersion uint64
	snapshot        []byte
}

func New(s *service.Service, c config.Config, log *slog.Logger) (*gin.Engine, error) {
	if log == nil {
		log = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if e := r.SetTrustedProxies(c.TrustedProxies); e != nil {
		return nil, e
	}
	a := &API{service: s, config: c, log: log, streams: make(chan struct{}, 64)}
	r.Use(logger(log), gin.CustomRecoveryWithWriter(io.Discard, func(ctx *gin.Context, _ any) {
		log.Error("request panic", "path", ctx.Request.URL.Path)
		fail(ctx, 500, "internal_error", "Internal server error")
		ctx.Abort()
	}), cors(c))
	r.NoRoute(func(c *gin.Context) { fail(c, 404, "not_found", "Resource not found") })
	r.GET("/health", func(c *gin.Context) { success(c, gin.H{"status": "ok", "version": "0.1.0"}) })
	admin := r.Group("/api/v1/admin")
	admin.POST("/auth/login", a.login)
	secure := admin.Group("")
	secure.Use(authenticated(s))
	secure.POST("/auth/logout", a.logout)
	secure.GET("/auth/me", a.me)
	secure.GET("/nodes", a.nodes)
	secure.POST("/nodes", a.createNode)
	secure.GET("/nodes/:id", a.node)
	secure.PATCH("/nodes/:id", a.updateNode)
	secure.DELETE("/nodes/:id", a.deleteNode)
	secure.GET("/brands", a.labels("brands"))
	secure.GET("/groups", a.labels("groups"))
	secure.PUT("/nodes/:id/billing", a.billing)
	secure.PUT("/nodes/:id/traffic", a.traffic)
	secure.GET("/nodes/:id/install", a.install)
	secure.PUT("/nodes/:id/latency-tasks", a.assign)
	secure.GET("/latency/tasks", a.tasks)
	secure.POST("/latency/tasks", a.saveTask)
	secure.PATCH("/latency/tasks/:id", a.saveTask)
	secure.DELETE("/latency/tasks/:id", a.deleteTask)
	secure.GET("/channels", a.channels)
	secure.POST("/channels", a.saveChannel)
	secure.PATCH("/channels/:id", a.saveChannel)
	secure.DELETE("/channels/:id", a.deleteChannel)
	secure.POST("/channels/test", a.testChannel)
	secure.POST("/channels/:id/test", a.testSavedChannel)
	secure.GET("/settings", a.settings)
	secure.PUT("/settings", a.saveSettings)
	secure.POST("/geoip/lookup", a.geoip)
	agent := r.Group("/api/v1/agent")
	agent.Use(agentAuthenticated(s))
	agent.POST("/info", a.agentInfo)
	agent.POST("/report", a.report)
	agent.GET("/config", a.agentConfig)
	agent.POST("/latency-results", a.latencyResults)
	public := r.Group("/api/v1/public")
	public.GET("/settings", a.publicSettings)
	public.GET("/nodes", a.publicNodes)
	public.GET("/nodes/:id", a.publicNode)
	public.GET("/nodes/:id/history", a.history)
	public.GET("/nodes/:id/latency", a.latency)
	public.GET("/events", a.events)
	return r, nil
}
func success(c *gin.Context, value any) {
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": value})
}
func fail(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"status": "error", "code": code, "message": message})
}
func respondError(c *gin.Context, e error) {
	var validation model.ValidationError
	switch {
	case errors.As(e, &validation):
		fail(c, 400, "invalid_request", validation.Error())
	case errors.Is(e, model.ErrNotFound):
		fail(c, 404, "not_found", "Resource not found")
	case errors.Is(e, model.ErrConflict):
		fail(c, 409, "conflict", "Resource conflict or stale report")
	case errors.Is(e, model.ErrUnauthorized):
		fail(c, 401, "unauthorized", "Invalid credentials or expired session")
	default:
		slog.Error("request failed", "path", c.Request.URL.Path, "errorType", fmt.Sprintf("%T", e))
		fail(c, 500, "internal_error", "Internal server error")
	}
}
func bind(c *gin.Context, value any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(value); e != nil {
		var size *http.MaxBytesError
		if errors.As(e, &size) {
			fail(c, 413, "body_too_large", "Request body exceeds 1 MiB")
		} else {
			fail(c, 400, "invalid_json", "Invalid JSON or unknown request fields")
		}
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(c, 400, "invalid_json", "Expected one JSON object")
		return false
	}
	return true
}
func reply[T any](c *gin.Context, value T, e error) {
	if e != nil {
		respondError(c, e)
	} else {
		success(c, value)
	}
}
func done(c *gin.Context, e error) {
	if e != nil {
		respondError(c, e)
	} else {
		success(c, nil)
	}
}
func (a *API) sessionCookie(c *gin.Context, token string, remove bool) {
	age := int(service.SessionDuration / time.Second)
	expires := a.service.Now().Add(service.SessionDuration)
	if remove {
		age = -1
		expires = time.Unix(1, 0)
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: cookieName, Value: token, Path: "/api/v1/admin", Secure: a.config.CookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age, Expires: expires})
}
func (a *API) login(c *gin.Context) {
	if !a.limiter.allow(c.ClientIP()) {
		c.Header("Retry-After", "900")
		fail(c, 429, "rate_limited", "Too many login attempts; try again later")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &input) {
		return
	}
	if len(input.Username) > 240 || len(input.Password) > 72 {
		respondError(c, model.ErrUnauthorized)
		return
	}
	token, e := a.service.Login(c.Request.Context(), input.Username, input.Password)
	if e != nil {
		respondError(c, e)
		return
	}
	a.limiter.reset(c.ClientIP())
	a.sessionCookie(c, token, false)
	a.me(c)
}
func (a *API) logout(c *gin.Context) {
	token, _ := c.Cookie(cookieName)
	if e := a.service.Logout(c.Request.Context(), token); e != nil {
		respondError(c, e)
		return
	}
	a.sessionCookie(c, "", true)
	success(c, nil)
}
func (a *API) me(c *gin.Context) {
	name, e := a.service.Username(c.Request.Context())
	reply(c, gin.H{"name": name, "role": "owner"}, e)
}
