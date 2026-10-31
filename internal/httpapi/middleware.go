package httpapi

import (
	"log/slog"
	"net/http"
	"noise-server/internal/config"
	"noise-server/internal/model"
	"noise-server/internal/service"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func cors(c config.Config) gin.HandlerFunc {
	admin := map[string]bool{}
	public := map[string]bool{}
	for _, v := range c.AdminOrigins {
		admin[v] = true
		public[v] = true
	}
	for _, v := range c.PublicOrigins {
		public[v] = true
	}
	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")
		private := strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/admin")
		allowed := public
		if private {
			allowed = admin
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		if origin != "" {
			ctx.Writer.Header().Add("Vary", "Origin")
			if !allowed[origin] {
				fail(ctx, http.StatusForbidden, "origin_not_allowed", "Origin is not allowed")
				ctx.Abort()
				return
			}
			ctx.Header("Access-Control-Allow-Origin", origin)
			if private {
				ctx.Header("Access-Control-Allow-Credentials", "true")
			}
		}
		if ctx.Request.Method == http.MethodOptions {
			if origin == "" {
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			ctx.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			ctx.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			ctx.Header("Access-Control-Max-Age", "600")
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}
		// A host-only SameSite cookie is supplemented with an Origin check on every admin mutation.
		if private && ctx.Request.Method != http.MethodGet && ctx.Request.Method != http.MethodHead && origin == "" {
			fail(ctx, http.StatusForbidden, "origin_required", "An allowed Origin header is required")
			ctx.Abort()
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 1<<20)
		ctx.Next()
	}
}
func authenticated(s *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _ := c.Cookie(cookieName)
		ok, e := s.Authenticated(c.Request.Context(), token)
		if e != nil {
			respondError(c, e)
			c.Abort()
			return
		}
		if !ok {
			respondError(c, model.ErrUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}
func agentAuthenticated(s *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondError(c, model.ErrUnauthorized)
			c.Abort()
			return
		}
		node, e := s.AgentNode(c.Request.Context(), parts[1])
		if e != nil {
			respondError(c, e)
			c.Abort()
			return
		}
		c.Set("nodeID", node.ID)
		c.Next()
	}
}
func logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http request", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration", time.Since(start))
	}
}

type attempt struct {
	start time.Time
	count int
}
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]attempt
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.entries == nil {
		l.entries = map[string]attempt{}
	}
	a := l.entries[ip]
	if now.Sub(a.start) > 15*time.Minute {
		a = attempt{start: now}
	}
	if a.count >= 10 {
		return false
	}
	if len(l.entries) >= 10000 {
		for key, v := range l.entries {
			if now.Sub(v.start) > 15*time.Minute {
				delete(l.entries, key)
			}
		}
		if _, exists := l.entries[ip]; !exists && len(l.entries) >= 10000 {
			return false
		}
	}
	a.count++
	l.entries[ip] = a
	return true
}
func (l *loginLimiter) reset(ip string) { l.mu.Lock(); defer l.mu.Unlock(); delete(l.entries, ip) }
