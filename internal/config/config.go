package config

import (
	"os"
	"strconv"
	"strings"

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
	Docs struct {
		MaxLLMSections      int  `yaml:"max_llm_sections"`
		EnableSemanticMatch bool `yaml:"enable_semantic_match"`
		EnableLLMRouter     bool `yaml:"enable_llm_router"`
		MaxLLMRoutes        int  `yaml:"max_llm_routes"`
	} `yaml:"docs"`
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

	// Docs runtime options with env overrides
	if v := os.Getenv("DOCOD_MAX_LLM_SECTIONS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Docs.MaxLLMSections = n
		}
	}
	if v := os.Getenv("DOCOD_ENABLE_SEMANTIC_MATCH"); v != "" {
		cfg.Docs.EnableSemanticMatch = parseBool(v)
	}
	if v := os.Getenv("DOCOD_ENABLE_LLM_ROUTER"); v != "" {
		cfg.Docs.EnableLLMRouter = parseBool(v)
	}
	if v := os.Getenv("DOCOD_MAX_LLM_ROUTES"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Docs.MaxLLMRoutes = n
		}
	}

	return &cfg, nil
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
