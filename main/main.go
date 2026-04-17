package main

import (
	"flag"
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

	svc := core.NewSearchService("default")
	for _, engine := range []core.Engine{
		core.NewKMPEngine(),
	} {
		svc.RegisterEngine(engine)
	}

	cfg := infra.LoadConfig(*configPath)
}
