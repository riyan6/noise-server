package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"noise-server/internal/config"
	"noise-server/internal/httpapi"
	"noise-server/internal/repository/sqlite"
	"noise-server/internal/scheduler"
	"noise-server/internal/service"
	"time"
)

func Run(parent context.Context, c config.Config, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	store, e := sqlite.Open(c.DatabasePath)
	if e != nil {
		return e
	}
	defer store.Close()
	s, e := service.New(ctx, store, c)
	if e != nil {
		return e
	}
	router, e := httpapi.New(s, c, log)
	if e != nil {
		return e
	}
	server := &http.Server{Addr: c.Address, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	listener, e := net.Listen("tcp", c.Address)
	if e != nil {
		return e
	}
	jobsDone := make(chan struct{})
	go func() { defer close(jobsDone); scheduler.Run(ctx, s, log) }()
	serving := make(chan error, 1)
	go func() {
		log.Info("noise-server starting", "address", listener.Addr().String(), "database", c.DatabasePath)
		serving <- server.Serve(listener)
	}()
	var serveError error
	select {
	case <-ctx.Done():
	case serveError = <-serving:
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	shutdownError := server.Shutdown(shutdown)
	if shutdownError != nil {
		_ = server.Close()
	}
	<-jobsDone
	if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
		return serveError
	}
	return shutdownError
}
