package scheduler

import (
	"context"
	"log/slog"
	"noise-server/internal/service"
	"time"
)

func Run(ctx context.Context, s *service.Service, log *slog.Logger) {
	run := func() {
		if e := s.Maintain(ctx); e != nil && ctx.Err() == nil {
			log.Error("scheduled maintenance failed", "error", e)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
