package config

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

func Load(configPath string) (*Config, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, fmt.Errorf("configPath is required")
	}

	f, err := os.OpenFile(configPath, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	cfg = setDefaults(cfg)

	return &cfg, nil
}

func setDefaults(cfg Config) Config {
	for i := range cfg.RPZs {
		if cfg.RPZs[i].TTL == 0 {
			cfg.RPZs[i].TTL = 30
		}
		if cfg.RPZs[i].Refresh == 0 {
			cfg.RPZs[i].Refresh = 3600
		}

		if cfg.RPZs[i].Retry == 0 {
			cfg.RPZs[i].Retry = 600
		}

		if cfg.RPZs[i].NegativeTTL == 0 {
			cfg.RPZs[i].NegativeTTL = 30
		}

		if cfg.RPZs[i].Expire == 0 {
			cfg.RPZs[i].Expire = 604800
		}
	}

	return cfg
}
