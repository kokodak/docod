package config

import (
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Project struct {
		Root string `yaml:"root"`
	} `yaml:"project"`
	AI struct {
		Provider     string `yaml:"provider"`
		Model        string `yaml:"model"`         // embedding model
		SummaryModel string `yaml:"summary_model"` // LLM model for summarization
		APIKey       string `yaml:"api_key"`
		Dimension    int    `yaml:"dimension"`
	} `yaml:"ai"`
}

func LoadConfig(path string) (*Config, error) {
	// 1. Load .env if exists
	_ = godotenv.Load()

	// 2. Load YAML config
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(file, &cfg); err != nil {
		return nil, err
	}

	// 3. Override with Environment Variables if present
	if apiKey := os.Getenv("DOCOD_API_KEY"); apiKey != "" {
		cfg.AI.APIKey = apiKey
	}
	if provider := os.Getenv("DOCOD_AI_PROVIDER"); provider != "" {
		cfg.AI.Provider = provider
	}

	return &cfg, nil
}
