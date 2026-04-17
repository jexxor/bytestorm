package infra

import (
	"go.uber.org/zap"
)

func SetupLog() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
}
