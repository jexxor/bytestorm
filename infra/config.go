package infra

import (
	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"
)

type Config struct {
	Server struct {
		Host string `yaml:"host" env:"SERVER_HOST" envDefault:"127.0.0.1"`
		Port int    `yaml:"port" env:"SERVER_PORT" envDefault:"8080"`
	}
}

var BytestormConfig = Config{}

func LoadConfig(path string) Config {
	err := cleanenv.ReadConfig(path, &BytestormConfig)
	if err != nil {
		zap.S().Sync()
		zap.S().Fatalf("Failed to load config: %v", err)
	}

	return BytestormConfig
}
