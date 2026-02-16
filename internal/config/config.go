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
		EmbeddingProvider string `yaml:"embedding_provider"`
		EmbeddingModel    string `yaml:"embedding_model"`
		EmbeddingAPIKey   string `yaml:"embedding_api_key"`
		EmbeddingDim      int    `yaml:"embedding_dimension"`
		LLMProvider       string `yaml:"llm_provider"`
		LLMModel          string `yaml:"llm_model"`
		LLMAPIKey         string `yaml:"llm_api_key"`
		OpenAIBaseURL     string `yaml:"openai_base_url"`
		LLMBaseURL        string `yaml:"llm_base_url"`
		OllamaBaseURL     string `yaml:"ollama_base_url"`
	} `yaml:"ai"`
	Docs struct {
		MaxLLMSections       int     `yaml:"max_llm_sections"`
		EnableSemanticMatch  bool    `yaml:"enable_semantic_match"`
		EnableLLMRouter      bool    `yaml:"enable_llm_router"`
		MaxLLMRoutes         int     `yaml:"max_llm_routes"`
		MinConfidenceForLLM  float64 `yaml:"min_confidence_for_llm"`
		MaxEmbedChunksPerRun int     `yaml:"max_embed_chunks_per_run"`
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
	if provider := os.Getenv("DOCOD_EMBEDDING_PROVIDER"); provider != "" {
		cfg.AI.EmbeddingProvider = provider
	}
	if model := os.Getenv("DOCOD_EMBEDDING_MODEL"); model != "" {
		cfg.AI.EmbeddingModel = model
	}
	if key := os.Getenv("DOCOD_EMBEDDING_API_KEY"); key != "" {
		cfg.AI.EmbeddingAPIKey = key
	}
	if dim := os.Getenv("DOCOD_EMBEDDING_DIMENSION"); dim != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(dim)); err == nil {
			cfg.AI.EmbeddingDim = n
		}
	}
	if provider := os.Getenv("DOCOD_LLM_PROVIDER"); provider != "" {
		cfg.AI.LLMProvider = provider
	}
	if model := os.Getenv("DOCOD_LLM_MODEL"); model != "" {
		cfg.AI.LLMModel = model
	}
	if llmKey := os.Getenv("DOCOD_LLM_API_KEY"); llmKey != "" {
		cfg.AI.LLMAPIKey = llmKey
	}
	if baseURL := os.Getenv("DOCOD_OPENAI_BASE_URL"); baseURL != "" {
		cfg.AI.OpenAIBaseURL = baseURL
	}
	if baseURL := os.Getenv("DOCOD_LLM_BASE_URL"); baseURL != "" {
		cfg.AI.LLMBaseURL = baseURL
	}
	if baseURL := os.Getenv("DOCOD_OLLAMA_BASE_URL"); baseURL != "" {
		cfg.AI.OllamaBaseURL = baseURL
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
	if v := os.Getenv("DOCOD_MIN_CONFIDENCE_FOR_LLM"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			cfg.Docs.MinConfidenceForLLM = f
		}
	}
	if v := os.Getenv("DOCOD_MAX_EMBED_CHUNKS_PER_RUN"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Docs.MaxEmbedChunksPerRun = n
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
