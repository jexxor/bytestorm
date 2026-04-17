package infra

import (
	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"
)

const DefaultResultBufferCap = 64 << 10

type Config struct {
	Server struct {
		Host string `yaml:"host" env:"SERVER_HOST" envDefault:"127.0.0.1"`
		Port int    `yaml:"port" env:"SERVER_PORT" envDefault:"8080"`
	}
	Metrics struct {
		Host string `yaml:"host" env:"METRICS_HOST" envDefault:"0.0.0.0"`
		Port int    `yaml:"port" env:"METRICS_PORT" envDefault:"9090"`
	} `yaml:"metrics"`
	YDB struct {
		DSN   string `yaml:"dsn" env:"YDB_DSN" envDefault:""`
		Table string `yaml:"table" env:"YDB_TABLE" envDefault:"/local/bytestorm_stream_summary"`
	} `yaml:"ydb"`
	Engine struct {
		ResultBufferCap int `yaml:"result_buffer_cap" env:"ENGINE_RESULT_BUFFER_CAP" envDefault:"65536"`
	} `yaml:"engine"`
}

var BytestormConfig = Config{}

func LoadConfig(path string) Config {
	err := cleanenv.ReadConfig(path, &BytestormConfig)
	if err != nil {
		zap.S().Sync()
		zap.S().Fatalf("Failed to load config: %v", err)
	}

	if BytestormConfig.Engine.ResultBufferCap <= 0 {
		BytestormConfig.Engine.ResultBufferCap = DefaultResultBufferCap
	}

	return BytestormConfig
}
