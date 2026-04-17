package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

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

	svc := core.NewSearchService(core.KMPEngineID)
	for _, engine := range []core.Engine{
		core.NewKMPEngine(),
		core.NewSIMDEngine(),
		core.NewStdlibEngine(),
	} {
		svc.RegisterEngine(engine)
	}

	server := transport.NewGRPCServer(cfg.Server.Host, cfg.Server.Port, svc)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Start(ctx); err != nil {
		zap.S().Fatalf("Failed to start gRPC server: %v", err)
	}
}
