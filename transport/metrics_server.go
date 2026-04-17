package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"jexxor/bytestorm/infra"

	"go.uber.org/zap"
)

type MetricsServer struct {
	addr   string
	server *http.Server
}

func NewMetricsServer(host string, port int) *MetricsServer {
	addr := fmt.Sprintf("%s:%d", host, port)

	return &MetricsServer{
		addr:   addr,
		server: infra.NewMetricsServer(addr),
	}
}

func (s *MetricsServer) Start(ctx context.Context) error {
	_ = ctx

	zap.S().Infof("Metrics server listening on %s", s.addr)
	err := s.server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *MetricsServer) Stop(ctx context.Context) error {
	if err := infra.FlushMetrics(); err != nil {
		zap.S().Warnf("Failed to gather metrics on shutdown: %v", err)
	}

	err := s.server.Shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
