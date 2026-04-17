package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jexxor/bytestorm/core"
	"jexxor/bytestorm/infra"
	"jexxor/bytestorm/transport"

	"go.uber.org/zap"
)

func main() {
	infra.SetupLog()
	defer zap.S().Sync()

	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()
	cfg := infra.LoadConfig(*configPath)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()

	ydbClient, err := infra.NewYDBClient(startupCtx, cfg.YDB.DSN, cfg.YDB.Table)
	if err != nil {
		zap.S().Fatalf("Failed to initialize YDB client: %v", err)
	}

	svc := core.NewSearchService(core.KMPEngineID)
	for _, engine := range []core.Engine{
		core.NewKMPEngine(),
		core.NewSIMDEngine(cfg.Engine.ResultBufferCap),
		core.NewStdlibEngine(),
	} {
		svc.RegisterEngine(engine)
	}

	server := transport.NewGRPCServer(cfg.Server.Host, cfg.Server.Port, svc, ydbClient)

	metricsAddr := fmt.Sprintf("%s:%d", cfg.Metrics.Host, cfg.Metrics.Port)
	metricsServer := infra.NewMetricsServer(metricsAddr)
	go func() {
		if serveErr := metricsServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			zap.S().Fatalf("Failed to start metrics server: %v", serveErr)
		}
	}()
	zap.S().Infof("Metrics server listening on %s", metricsAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runErr := server.Start(ctx)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := infra.FlushMetrics(); err != nil {
		zap.S().Warnf("Failed to gather metrics on shutdown: %v", err)
	}

	if err := metricsServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		zap.S().Warnf("Failed to stop metrics server cleanly: %v", err)
	}

	if err := ydbClient.Close(shutdownCtx); err != nil {
		zap.S().Warnf("Failed to close YDB client cleanly: %v", err)
	}

	if runErr != nil {
		zap.S().Fatalf("Failed to start gRPC server: %v", runErr)
	}
}
