package config

// --- Tools, Gateway, and infrastructure configuration types ---

type GatewayConfig struct {
	Host string `json:"host,omitempty" env:"PICOCLAW_GATEWAY_HOST"`
	Port int    `json:"port,omitempty" env:"PICOCLAW_GATEWAY_PORT"`
}

type HeartbeatConfig struct {
	Enabled  bool `json:"enabled,omitempty"  env:"PICOCLAW_HEARTBEAT_ENABLED"`
	Interval int  `json:"interval,omitempty" env:"PICOCLAW_HEARTBEAT_INTERVAL"` // minutes, min 5
}

// LoggingConfig controls log output.
type LoggingConfig struct {
	Level     string `json:"level,omitempty"`      // debug, info, warn, error (default: warn)
	FileDir   string `json:"file_dir,omitempty"`   // directory for system log files; empty = no file logging
	PromptDir string `json:"prompt_dir,omitempty"` // directory for LLM prompt/response logs; empty = no prompt logging
}

type ToolsConfig struct {
	AllowReadPaths  []string           `json:"allow_read_paths,omitempty"  env:"PICOCLAW_TOOLS_ALLOW_READ_PATHS"`
	AllowWritePaths []string           `json:"allow_write_paths,omitempty" env:"PICOCLAW_TOOLS_ALLOW_WRITE_PATHS"`
	Web             WebToolsConfig     `json:"web,omitempty"`
	RAG             RAGToolsConfig     `json:"rag,omitempty"`
	Cron            CronToolsConfig    `json:"cron,omitempty"`
	Exec            ExecConfig         `json:"exec,omitempty"`
	Skills          SkillsToolsConfig  `json:"skills,omitempty"`
	MediaCleanup    MediaCleanupConfig `json:"media_cleanup,omitempty"`
	MCP             MCPConfig          `json:"mcp,omitempty"`
}

type RAGToolsConfig struct {
	Enabled        bool   `json:"enabled,omitempty"         env:"PICOCLAW_TOOLS_RAG_ENABLED"`
	EmbeddingModel string `json:"embedding_model,omitempty" env:"PICOCLAW_TOOLS_RAG_EMBEDDING_MODEL"`
}

type BraveConfig struct {
	Enabled    bool   `json:"enabled,omitempty"     env:"PICOCLAW_TOOLS_WEB_BRAVE_ENABLED"`
	APIKey     string `json:"api_key,omitempty"     env:"PICOCLAW_TOOLS_WEB_BRAVE_API_KEY"`
	MaxResults int    `json:"max_results,omitempty" env:"PICOCLAW_TOOLS_WEB_BRAVE_MAX_RESULTS"`
}

type TavilyConfig struct {
	Enabled    bool   `json:"enabled,omitempty"     env:"PICOCLAW_TOOLS_WEB_TAVILY_ENABLED"`
	APIKey     string `json:"api_key,omitempty"     env:"PICOCLAW_TOOLS_WEB_TAVILY_API_KEY"`
	BaseURL    string `json:"base_url,omitempty"    env:"PICOCLAW_TOOLS_WEB_TAVILY_BASE_URL"`
	MaxResults int    `json:"max_results,omitempty" env:"PICOCLAW_TOOLS_WEB_TAVILY_MAX_RESULTS"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled,omitempty"     env:"PICOCLAW_TOOLS_WEB_DUCKDUCKGO_ENABLED"`
	MaxResults int  `json:"max_results,omitempty" env:"PICOCLAW_TOOLS_WEB_DUCKDUCKGO_MAX_RESULTS"`
}

type PerplexityConfig struct {
	Enabled    bool   `json:"enabled,omitempty"     env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_ENABLED"`
	APIKey     string `json:"api_key,omitempty"     env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_API_KEY"`
	MaxResults int    `json:"max_results,omitempty" env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_MAX_RESULTS"`
}

type WebToolsConfig struct {
	Brave           BraveConfig      `json:"brave,omitempty"`
	Tavily          TavilyConfig     `json:"tavily,omitempty"`
	DuckDuckGo      DuckDuckGoConfig `json:"duckduckgo,omitempty"`
	Perplexity      PerplexityConfig `json:"perplexity,omitempty"`
	Proxy           string           `json:"proxy,omitempty"             env:"PICOCLAW_TOOLS_WEB_PROXY"`
	FetchLimitBytes int64            `json:"fetch_limit_bytes,omitempty" env:"PICOCLAW_TOOLS_WEB_FETCH_LIMIT_BYTES"`
}

type CronToolsConfig struct {
	ExecTimeoutMinutes int `json:"exec_timeout_minutes,omitempty" env:"PICOCLAW_TOOLS_CRON_EXEC_TIMEOUT_MINUTES"`
}

type ExecConfig struct {
	EnableDenyPatterns  bool     `json:"enable_deny_patterns,omitempty"  env:"PICOCLAW_TOOLS_EXEC_ENABLE_DENY_PATTERNS"`
	CustomDenyPatterns  []string `json:"custom_deny_patterns,omitempty"  env:"PICOCLAW_TOOLS_EXEC_CUSTOM_DENY_PATTERNS"`
	CustomAllowPatterns []string `json:"custom_allow_patterns,omitempty" env:"PICOCLAW_TOOLS_EXEC_CUSTOM_ALLOW_PATTERNS"`
	EnableLLMReview     bool     `json:"enable_llm_review,omitempty"     env:"PICOCLAW_TOOLS_EXEC_ENABLE_LLM_REVIEW"`
}

type MediaCleanupConfig struct {
	Enabled  bool `json:"enabled,omitempty"          env:"PICOCLAW_MEDIA_CLEANUP_ENABLED"`
	MaxAge   int  `json:"max_age_minutes,omitempty"  env:"PICOCLAW_MEDIA_CLEANUP_MAX_AGE"`
	Interval int  `json:"interval_minutes,omitempty" env:"PICOCLAW_MEDIA_CLEANUP_INTERVAL"`
}

type SkillsToolsConfig struct {
	Registries            SkillsRegistriesConfig `json:"registries,omitempty"`
	MaxConcurrentSearches int                    `json:"max_concurrent_searches,omitempty" env:"PICOCLAW_SKILLS_MAX_CONCURRENT_SEARCHES"`
	SearchCache           SearchCacheConfig      `json:"search_cache,omitempty"`
}

type SearchCacheConfig struct {
	MaxSize    int `json:"max_size,omitempty"    env:"PICOCLAW_SKILLS_SEARCH_CACHE_MAX_SIZE"`
	TTLSeconds int `json:"ttl_seconds,omitempty" env:"PICOCLAW_SKILLS_SEARCH_CACHE_TTL_SECONDS"`
}

type SkillsRegistriesConfig struct {
	ClawHub ClawHubRegistryConfig `json:"clawhub,omitempty"`
}

type ClawHubRegistryConfig struct {
	Enabled         bool   `json:"enabled,omitempty"                     env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_ENABLED"`
	BaseURL         string `json:"base_url,omitempty"                    env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_BASE_URL"`
	AuthToken       string `json:"auth_token,omitempty"                  env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_AUTH_TOKEN"`
	SearchPath      string `json:"search_path,omitempty"                 env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_SEARCH_PATH"`
	SkillsPath      string `json:"skills_path,omitempty"                 env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_SKILLS_PATH"`
	DownloadPath    string `json:"download_path,omitempty"               env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_DOWNLOAD_PATH"`
	Timeout         int    `json:"timeout,omitempty"                     env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_TIMEOUT"`
	MaxZipSize      int    `json:"max_zip_size,omitempty"                env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_ZIP_SIZE"`
	MaxResponseSize int    `json:"max_response_size,omitempty"           env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_RESPONSE_SIZE"`
}

// MCPServerConfig defines configuration for a single MCP server.
type MCPServerConfig struct {
	Enabled bool              `json:"enabled,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	EnvFile string            `json:"env_file,omitempty"`
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPConfig defines configuration for all MCP servers.
type MCPConfig struct {
	Enabled bool                       `json:"enabled,omitempty" env:"PICOCLAW_TOOLS_MCP_ENABLED"`
	Servers map[string]MCPServerConfig `json:"servers,omitempty"`
}
