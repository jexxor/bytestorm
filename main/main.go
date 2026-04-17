package main

import (
	"context"
	"flag"
	"time"

	"jexxor/bytestorm/core"
	"jexxor/bytestorm/infra"
	"jexxor/bytestorm/transport"

	"go.uber.org/zap"
)

const ydbLifecycleTimeout = 10 * time.Second

func main() {
	infra.SetupLog()
	defer zap.S().Sync()

	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()
	cfg := infra.LoadConfig(*configPath)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), ydbLifecycleTimeout)
	defer cancelStartup()

	ydbClient, err := infra.NewYDBClient(startupCtx, cfg.YDB.DSN, cfg.YDB.Table)
	if err != nil {
		zap.S().Fatalf("Failed to initialize YDB client: %v", err)
	}
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), ydbLifecycleTimeout)
		defer cancelShutdown()

		if closeErr := ydbClient.Close(shutdownCtx); closeErr != nil {
			zap.S().Warnf("Failed to close YDB client cleanly: %v", closeErr)
		}
	}()

	svc := newSearchService(cfg.Engine.ResultBufferCap)

	launcher := transport.NewLauncher(
		transport.NewGRPCServer(cfg.Server.Host, cfg.Server.Port, svc, ydbClient),
		transport.NewMetricsServer(cfg.Metrics.Host, cfg.Metrics.Port),
	)

	if err := launcher.Run(context.Background()); err != nil {
		zap.S().Fatalf("Launcher stopped with error: %v", err)
	}
}

func newSearchService(resultBufferCap int) *core.SearchService {
	simdEnabled := core.SIMDEnabled()
	infra.SetSIMDEnabled(simdEnabled)

	if !simdEnabled {
		zap.S().Warn("!!! ATTENTION !!! RUNNING ON ANCIENT HARDWARE. AVX2 NOT FOUND.")
		zap.S().Warn("SIMD ENGINE WILL FALLBACK TO SCALAR/KMP MODE. PERFORMANCE MAY DEGRADE.")
	}

	svc := core.NewSearchService(core.KMPEngineID)
	for _, engine := range []core.Engine{
		core.NewKMPEngine(),
		newSIMDEngineForHost(resultBufferCap, simdEnabled),
		core.NewStdlibEngine(),
	} {
		svc.RegisterEngine(engine)
	}

	return svc
}

func newSIMDEngineForHost(resultBufferCap int, simdEnabled bool) core.Engine {
	if simdEnabled {
		return core.NewSIMDEngine(resultBufferCap)
	}

	return core.NewSIMDFallbackEngine(core.NewKMPEngine())
}
