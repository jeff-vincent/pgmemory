package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeff-vincent/pgmemory/internal/credential"
	"gopkg.in/yaml.v3"
)

// Mode constants control how the daemon integrates with agents.
const (
	ModeProxy       = "proxy"        // Proxy intercepts API calls — auto read + write
	ModeMCP         = "mcp"          // MCP tools only — explicit read + write
	ModeMCPReadOnly = "mcp-readonly" // MCP tools, search only — no writes
)

// PipelineConfig tunes the write pipeline quality filters.
// All values are live-reloadable via the dashboard — changes take effect
// immediately without restarting the daemon. Prototype changes trigger a
// background scorer reload.
type PipelineConfig struct {
	// Noise filter
	NoiseMinLen        int     `yaml:"noise_min_len"         json:"noise_min_len"`
	NoiseMinAlnumRatio float64 `yaml:"noise_min_alnum_ratio" json:"noise_min_alnum_ratio"`

	// Pre-Haiku gates — applied to raw assistant text before the LLM call.
	// IngestMinLen: assistant responses shorter than this skip Haiku entirely.
	IngestMinLen int `yaml:"ingest_min_len" json:"ingest_min_len"`
	// ContentScorePreGate: if > 0, embed the raw text and score it against
	// noise prototypes. Responses scoring below this skip Haiku.
	ContentScorePreGate float64 `yaml:"content_score_pre_gate" json:"content_score_pre_gate"`

	// Content score gate — chunks scoring below this threshold are dropped
	// before storage. Set to 0 to disable (default).
	ContentScoreGate float64 `yaml:"content_score_gate" json:"content_score_gate"`

	// Deduplication
	DedupThreshold           float64 `yaml:"dedup_threshold"            json:"dedup_threshold"`
	SourceExtensionThreshold float64 `yaml:"source_extension_threshold" json:"source_extension_threshold"`

	// Topic grouping
	TopicBoundaryThreshold float64 `yaml:"topic_boundary_threshold" json:"topic_boundary_threshold"`
	MaxGroupChars          int     `yaml:"max_group_chars"          json:"max_group_chars"`

	// Content scorer prototypes — empty means use built-in defaults.
	QualityProtos []string `yaml:"quality_protos,omitempty" json:"quality_protos,omitempty"`
	NoiseProtos   []string `yaml:"noise_protos,omitempty"   json:"noise_protos,omitempty"`
}

// Config holds all daemon configuration.
type Config struct {
	Port                 int            `yaml:"port"`
	Mode                 string         `yaml:"mode"`
	APIToken             string         `yaml:"-"` // loaded from ~/.pgmemory/token at startup, not stored in YAML
	PostgresURL          string         `yaml:"postgres_url,omitempty"`
	ModelPath            string         `yaml:"model_path"`
	EmbeddingDim         int            `yaml:"embedding_dim"`
	RetrievalTopK        int            `yaml:"retrieval_top_k"`
	RetrievalMaxTokens   int            `yaml:"retrieval_max_tokens"`
	UpstreamAnthropicURL string         `yaml:"upstream_anthropic_url"`
	LLMSynthesis         bool           `yaml:"llm_synthesis"`
	Steward              StewardConfig  `yaml:"steward"`
	Pipeline             PipelineConfig `yaml:"pipeline"`
	Prompts              PromptsConfig  `yaml:"prompts,omitempty"`
}

// PromptsConfig holds optional user-customised prompt templates.
// Empty strings mean "use the built-in default."
type PromptsConfig struct {
	QA           string `yaml:"qa,omitempty"           json:"qa,omitempty"`
	Merge        string `yaml:"merge,omitempty"        json:"merge,omitempty"`
	Conversation string `yaml:"conversation,omitempty" json:"conversation,omitempty"`
}

// StewardConfig tunes the background memory consolidation behaviour.
type StewardConfig struct {
	IntervalMinutes  int     `yaml:"interval_minutes"   json:"interval_minutes"`
	PruneThreshold   float64 `yaml:"prune_threshold"    json:"prune_threshold"`
	GracePeriodHours int     `yaml:"grace_period_hours" json:"grace_period_hours"`
	DecayHalfDays    int     `yaml:"decay_half_days"    json:"decay_half_days"`
	MergeThreshold   float64 `yaml:"merge_threshold"    json:"merge_threshold"`
	BatchSize        int     `yaml:"batch_size"         json:"batch_size"`
}

var Default = Config{
	Port:                 7432,
	Mode:                 ModeProxy,
	ModelPath:            "~/.pgmemory/models/voyage-4-nano.gguf",
	EmbeddingDim:         1024,
	RetrievalTopK:        5,
	RetrievalMaxTokens:   2048,
	UpstreamAnthropicURL: "https://api.anthropic.com",
	Steward: StewardConfig{
		IntervalMinutes:  60,
		PruneThreshold:   0.1,
		GracePeriodHours: 24,
		DecayHalfDays:    90,
		MergeThreshold:   0.88,
		BatchSize:        500,
	},
	Pipeline: PipelineConfig{
		NoiseMinLen:              40, // eval-tuned: 40 chars captures short-but-real agent responses without admitting trivial noise
		NoiseMinAlnumRatio:       0.40,
		IngestMinLen:             80,   // responses < 80 chars never contain durable knowledge (benchmark: 0% store rate < 100 chars)
		ContentScorePreGate:      0.35, // pre-Haiku noise gate: below this the raw text is too noise-like to warrant an LLM call
		ContentScoreGate:         0.0,
		DedupThreshold:           0.92,
		SourceExtensionThreshold: 0.75,
		TopicBoundaryThreshold:   0.65,
		MaxGroupChars:            2048,
	},
}

// Dir returns the pgmemory config directory path.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pgmemory")
}

// Path returns the config file path.
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads the config file, falling back to defaults for missing fields.
func Load() (*Config, error) {
	cfg := Default

	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.ModelPath = expandHome(cfg.ModelPath)

	// Resolve credentials from OS keychain.
	cfg.PostgresURL = resolveCredential(cfg.PostgresURL, "postgres_url")

	return &cfg, nil
}

// EnsureDir creates the config directory if it doesn't exist.
func EnsureDir() error {
	return os.MkdirAll(Dir(), 0700)
}

// WriteDefault writes a default config file if one doesn't already exist.
func WriteDefault() error {
	if err := EnsureDir(); err != nil {
		return err
	}

	path := Path()
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	data, err := yaml.Marshal(Default)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// ToStewardDuration helpers — convert the yaml-friendly int fields to time.Duration.
func (sc StewardConfig) Interval() time.Duration {
	return time.Duration(sc.IntervalMinutes) * time.Minute
}
func (sc StewardConfig) GracePeriod() time.Duration {
	return time.Duration(sc.GracePeriodHours) * time.Hour
}
func (sc StewardConfig) DecayHalfLife() time.Duration {
	return time.Duration(sc.DecayHalfDays) * 24 * time.Hour
}

// ProxyWriteEnabled returns true when the proxy should capture conversations.
func (c *Config) ProxyWriteEnabled() bool {
	return c.Mode == "" || c.Mode == ModeProxy
}

// MCPReadOnly returns true when MCP tools should be limited to reads.
func (c *Config) MCPReadOnly() bool {
	return c.Mode == ModeMCPReadOnly
}

// ValidMode returns true if the mode string is recognized.
func ValidMode(mode string) bool {
	switch mode {
	case ModeProxy, ModeMCP, ModeMCPReadOnly:
		return true
	}
	return false
}

// SetMode updates the mode in the config file on disk.
func SetMode(mode string) error {
	if !ValidMode(mode) {
		return fmt.Errorf("invalid mode %q: must be proxy, mcp, or mcp-readonly", mode)
	}

	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	cfg.Mode = mode
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

// SavePipelineConfig persists the pipeline config to the config file.
func SavePipelineConfig(pipelineCfg PipelineConfig) error {
	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	cfg.Pipeline = pipelineCfg
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

// SaveStewardConfig persists the steward config to the config file.
func SaveStewardConfig(stewardCfg StewardConfig) error {
	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	cfg.Steward = stewardCfg
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

// SavePromptsConfig persists custom prompt templates to the config file.
func SavePromptsConfig(promptsCfg PromptsConfig) error {
	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	cfg.Prompts = promptsCfg
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

func expandHome(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

// keychainPrefix is the sentinel value in config that triggers keychain lookup.
const keychainPrefix = "keychain:"

// resolveCredential checks if a config value is a keychain reference.
// If it starts with "keychain:" or equals "keychain", the actual value is
// fetched from the OS keychain. Otherwise the value is returned as-is.
func resolveCredential(value, key string) string {
	if value == "keychain" || strings.HasPrefix(value, keychainPrefix) {
		resolved, err := credential.Get(key)
		if err != nil || resolved == "" {
			return "" // let caller handle missing URI
		}
		return resolved
	}
	return value
}

// StoreCredential saves a credential in the OS keychain.
func StoreCredential(key, value string) error {
	return credential.Set(key, value)
}

// GetAnthropicAPIKey returns the Anthropic API key, checking the OS
// keychain first, then falling back to the ANTHROPIC_API_KEY env var.
func GetAnthropicAPIKey() string {
	if key, err := credential.Get("anthropic_api_key"); err == nil && key != "" {
		return key
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}

// SaveAnthropicAPIKey stores the API key in the keychain and enables
// llm_synthesis in the config file so the daemon picks it up on restart.
func SaveAnthropicAPIKey(key string) error {
	if err := credential.Set("anthropic_api_key", key); err != nil {
		return err
	}
	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}
	cfg.LLMSynthesis = true
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

// SavePostgresURL stores the Postgres connection URL in the keychain and
// updates the config file to use a keychain sentinel.
func SavePostgresURL(url string) error {
	if err := credential.Set("postgres_url", url); err != nil {
		return err
	}
	cfg := Default
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}
	cfg.PostgresURL = "keychain:postgres_url"
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := EnsureDir(); err != nil {
		return err
	}
	return os.WriteFile(Path(), out, 0600)
}

// UsePostgres returns true if the config has a postgres_url set,
// meaning the user wants to use external/team Postgres (not embedded).
func (c *Config) UsePostgres() bool {
	return c.PostgresURL != ""
}

// DeleteCredentials removes all pgmemory credentials from the OS keychain.
func DeleteCredentials() {
	_ = credential.Delete("anthropic_api_key")
	_ = credential.Delete("postgres_url")
}
