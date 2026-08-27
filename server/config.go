package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
)

const (
	serverEnvironmentPrefix = "POWERCONTEXT_SERVER_"
	PowerContextHomeEnv     = "POWERCONTEXT_HOME"
	DefaultMCPPath          = "/mcp"
)

var externalSkillTargetIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ProcessConfig is the validated process-owned configuration. Domain packages
// intentionally never read it or inspect the environment.
type ProcessConfig struct {
	HTTP           HTTPConfig
	MCP            MCPConfig
	Auth           AuthConfig
	Dashboard      DashboardConfig
	Logging        LoggingConfig
	Metrics        MetricsConfig
	Tracing        TracingConfig
	Runtime        RuntimeConfig
	Database       DatabaseConfig
	HandoffReport  HandoffReportConfig
	Inference      InferenceConfig
	ExternalSkills ExternalSkillsConfig
	SchedulerPath  string
}

type HTTPConfig struct {
	Host string
	Port int
}

func (c HTTPConfig) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

type MCPConfig struct {
	Enabled bool
	Path    string
}

type AuthConfig struct {
	Enabled bool
	Token   string `json:"-"`
}

func (c AuthConfig) String() string   { return c.redactedString() }
func (c AuthConfig) GoString() string { return c.redactedString() }

func (c AuthConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("enabled", c.Enabled),
		slog.Bool("token_configured", c.Token != ""),
	)
}

func (c AuthConfig) redactedString() string {
	token := "<unset>"
	if c.Token != "" {
		token = "<redacted>"
	}
	return fmt.Sprintf("{Enabled:%t Token:%s}", c.Enabled, token)
}

type DashboardScope struct {
	ScopeID     string `json:"scope_id"`
	DisplayName string `json:"display_name"`
}

type DashboardConfig struct {
	Enabled bool
	Scopes  []DashboardScope
}

type LoggingConfig struct {
	Level  string
	Format string
	Access bool
}

type MetricsConfig struct{ Enabled bool }
type TracingConfig struct{ Enabled bool }

type RuntimeConfig struct {
	ScopeCacheSize               int
	SourceWindowLimit            int64
	MemoryExtractionProfile      memory.ExtractionProfile
	MemoryRerankEnabled          bool
	MemoryRerankCandidateLimit   int
	SourceWindowInterval         *time.Duration
	ExperienceIncubationInterval *time.Duration
}

type DatabaseConfig struct {
	Kind      string
	SQLite    SQLiteDatabaseConfig
	OceanBase OceanBaseDatabaseConfig
	SeekDB    SeekDBDatabaseConfig
}

type SQLiteDatabaseConfig struct {
	URL             string
	BusyTimeout     time.Duration
	JournalMode     string
	ForeignKeys     bool
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type OceanBaseDatabaseConfig struct {
	URL          string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
}

type SeekDBDatabaseConfig struct {
	Path         string
	Database     string
	LibraryPath  string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
}

type HandoffReportConfig struct{ Enabled bool }

type InferenceConfig struct {
	GenerationModel        string
	GenerationTimeout      time.Duration
	GenerationMaxRequests  int
	EmbeddingModel         string
	EmbeddingProfileID     string
	EmbeddingDimension     int
	EmbeddingNormalization string
	EmbeddingTimeout       time.Duration
	EmbeddingBatchSize     int
}

type ExternalSkillRoot struct {
	RootID              string `json:"root_id"`
	InstallationScope   string `json:"installation_scope"`
	Path                string `json:"path"`
	AllowManagedPublish bool   `json:"allow_managed_publish"`
}

type ExternalSkillTarget struct {
	TargetID            string `json:"target_id"`
	AgentKind           string `json:"agent_kind"`
	InstallationScope   string `json:"installation_scope"`
	Path                string `json:"path"`
	AllowManagedPublish bool   `json:"allow_managed_publish"`
}

type ExternalSkillsConfig struct {
	HostID     string
	Targets    []ExternalSkillTarget
	CodexRoots []ExternalSkillRoot
}

type environmentConfig struct {
	HTTPHost string `env:"HTTP_HOST"`
	HTTPPort int    `env:"HTTP_PORT"`

	MCPEnabled bool   `env:"MCP_ENABLED"`
	MCPPath    string `env:"MCP_PATH"`

	AuthEnabled bool   `env:"AUTH_ENABLED"`
	AuthToken   string `env:"AUTH_TOKEN"`

	DashboardEnabled bool   `env:"DASHBOARD_ENABLED"`
	DashboardScopes  string `env:"DASHBOARD_SCOPES"`

	LoggingLevel   string `env:"LOGGING_LEVEL"`
	LoggingFormat  string `env:"LOGGING_FORMAT"`
	LoggingAccess  bool   `env:"LOGGING_ACCESS"`
	MetricsEnabled bool   `env:"METRICS_ENABLED"`
	TracingEnabled bool   `env:"TRACING_ENABLED"`

	ScopeCacheSize             int    `env:"RUNTIME_SCOPE_CACHE_SIZE"`
	SourceWindowLimit          int64  `env:"RUNTIME_SOURCE_WINDOW_LIMIT"`
	MemoryExtractionProfile    string `env:"RUNTIME_MEMORY_EXTRACTION_PROFILE"`
	MemoryRerankEnabled        bool   `env:"RUNTIME_MEMORY_RERANK_ENABLED"`
	MemoryRerankCandidateLimit int    `env:"RUNTIME_MEMORY_RERANK_CANDIDATE_LIMIT"`
	ScheduleSeconds            string `env:"RUNTIME_SCHEDULE_SECONDS"`
	ExperienceScheduleSeconds  string `env:"RUNTIME_EXPERIENCE_SCHEDULE_SECONDS"`

	DatabaseKind          string `env:"DATABASE_KIND"`
	DatabaseURL           string `env:"DATABASE_URL"`
	DatabasePath          string `env:"DATABASE_PATH"`
	DatabaseName          string `env:"DATABASE_DATABASE"`
	DatabaseLibraryPath   string `env:"DATABASE_LIBRARY_PATH"`
	DatabaseBusyTimeoutMS int64  `env:"DATABASE_BUSY_TIMEOUT_MS"`
	DatabaseJournalMode   string `env:"DATABASE_JOURNAL_MODE"`
	DatabaseForeignKeys   bool   `env:"DATABASE_FOREIGN_KEYS"`
	DatabaseMaxOpenConns  int    `env:"DATABASE_MAX_OPEN_CONNS"`
	DatabaseMaxIdleConns  int    `env:"DATABASE_MAX_IDLE_CONNS"`
	DatabaseMaxLifetime   string `env:"DATABASE_MAX_LIFETIME"`

	HandoffReportEnabled bool `env:"HANDOFF_REPORT_ENABLED"`

	GenerationModel          string `env:"INFERENCE_GENERATION_MODEL"`
	GenerationTimeoutSeconds string `env:"INFERENCE_GENERATION_TIMEOUT_SECONDS"`
	GenerationMaxRequests    int    `env:"INFERENCE_GENERATION_MAX_REQUESTS"`
	EmbeddingModel           string `env:"INFERENCE_EMBEDDING_MODEL"`
	EmbeddingProfileID       string `env:"INFERENCE_EMBEDDING_PROFILE_ID"`
	EmbeddingDimension       int    `env:"INFERENCE_EMBEDDING_DIMENSION"`
	EmbeddingNormalization   string `env:"INFERENCE_EMBEDDING_NORMALIZATION"`
	EmbeddingTimeoutSeconds  string `env:"INFERENCE_EMBEDDING_TIMEOUT_SECONDS"`
	EmbeddingBatchSize       int    `env:"INFERENCE_EMBEDDING_BATCH_SIZE"`
	ExternalSkills           string `env:"EXTERNAL_SKILLS"`
	ExternalSkillHostID      string `env:"EXTERNAL_SKILLS_HOST_ID"`
	ExternalSkillTargets     string `env:"EXTERNAL_SKILLS_TARGETS"`
	ExternalSkillRoots       string `env:"EXTERNAL_SKILLS_CODEX_ROOTS"`
	SchedulerPath            string `env:"SCHEDULER_PATH"`
}

type externalSkillsEnvironment struct {
	HostID     string                `json:"host_id"`
	Targets    []ExternalSkillTarget `json:"targets"`
	CodexRoots []ExternalSkillRoot   `json:"codex_roots"`
}

// LoadConfig overlays POWERCONTEXT_SERVER_* values on the frozen Python
// defaults. Nested object fields use the same one-level underscore spelling;
// slices use JSON, matching pydantic-settings' environment representation.
func LoadConfig() (ProcessConfig, error) {
	defaults, err := defaultEnvironmentConfig()
	if err != nil {
		return ProcessConfig{}, err
	}
	if err := env.ParseWithOptions(&defaults, env.Options{Prefix: serverEnvironmentPrefix}); err != nil {
		return ProcessConfig{}, fmt.Errorf("server: invalid environment configuration: %w", err)
	}
	return buildProcessConfig(defaults)
}

// DefaultConfig returns the same validated defaults used by LoadConfig,
// without consulting POWERCONTEXT_SERVER_* overrides.
func DefaultConfig() (ProcessConfig, error) {
	defaults, err := defaultEnvironmentConfig()
	if err != nil {
		return ProcessConfig{}, err
	}
	return buildProcessConfig(defaults)
}

func defaultEnvironmentConfig() (environmentConfig, error) {
	databasePath, err := DefaultDatabasePath()
	if err != nil {
		return environmentConfig{}, err
	}
	schedulerPath, err := DefaultSchedulerPath()
	if err != nil {
		return environmentConfig{}, err
	}
	seekDBPath, err := DefaultSeekDBPath()
	if err != nil {
		return environmentConfig{}, err
	}
	return environmentConfig{
		HTTPHost: "127.0.0.1", HTTPPort: 8000,
		MCPEnabled: true, MCPPath: DefaultMCPPath,
		DashboardEnabled: true, HandoffReportEnabled: true,
		LoggingLevel: "INFO", LoggingFormat: "console", LoggingAccess: true,
		MetricsEnabled: true,
		ScopeCacheSize: 128, SourceWindowLimit: 100, MemoryExtractionProfile: string(memory.CodingProfile),
		MemoryRerankCandidateLimit: 30,
		DatabaseKind:               "sqlite", DatabaseURL: sqliteURL(databasePath),
		DatabasePath: seekDBPath, DatabaseName: "test",
		DatabaseBusyTimeoutMS: 5_000, DatabaseJournalMode: "WAL", DatabaseForeignKeys: true,
		DatabaseMaxOpenConns: 8, DatabaseMaxIdleConns: 8,
		GenerationTimeoutSeconds: "30", GenerationMaxRequests: 2,
		EmbeddingNormalization: "unit", EmbeddingTimeoutSeconds: "30", EmbeddingBatchSize: 10,
		SchedulerPath: schedulerPath,
	}, nil
}

func buildProcessConfig(value environmentConfig) (ProcessConfig, error) {
	mcpPath, err := normalizeMCPPath(value.MCPPath)
	if err != nil {
		return ProcessConfig{}, err
	}
	generationTimeout, err := positiveSeconds("inference.generation_timeout_seconds", value.GenerationTimeoutSeconds)
	if err != nil {
		return ProcessConfig{}, err
	}
	embeddingTimeout, err := positiveSeconds("inference.embedding_timeout_seconds", value.EmbeddingTimeoutSeconds)
	if err != nil {
		return ProcessConfig{}, err
	}
	sourceSchedule, err := optionalPositiveSeconds("runtime.schedule_seconds", value.ScheduleSeconds)
	if err != nil {
		return ProcessConfig{}, err
	}
	experienceSchedule, err := optionalPositiveSeconds("runtime.experience_schedule_seconds", value.ExperienceScheduleSeconds)
	if err != nil {
		return ProcessConfig{}, err
	}
	maxLifetime, err := optionalDuration("database.max_lifetime", value.DatabaseMaxLifetime)
	if err != nil {
		return ProcessConfig{}, err
	}
	seekDBPath := strings.TrimSpace(value.DatabasePath)
	if seekDBPath == "" {
		seekDBPath, err = DefaultSeekDBPath()
		if err != nil {
			return ProcessConfig{}, err
		}
	}

	var scopes []DashboardScope
	if strings.TrimSpace(value.DashboardScopes) != "" {
		if err := decodeJSONArray(value.DashboardScopes, &scopes); err != nil {
			return ProcessConfig{}, fmt.Errorf("server: dashboard.scopes must be a JSON array: %w", err)
		}
		for index := range scopes {
			// Pydantic applies the declared length bounds to the input and then
			// strips these two fields in its after-validator.
			if len([]rune(scopes[index].ScopeID)) > 255 || len([]rune(scopes[index].DisplayName)) > 80 {
				return ProcessConfig{}, errors.New("server: Dashboard scope values are invalid")
			}
			scopes[index].ScopeID = strings.TrimSpace(scopes[index].ScopeID)
			scopes[index].DisplayName = strings.TrimSpace(scopes[index].DisplayName)
		}
	}
	externalHostID := value.ExternalSkillHostID
	var targets []ExternalSkillTarget
	var roots []ExternalSkillRoot
	if strings.TrimSpace(value.ExternalSkills) != "" {
		if value.ExternalSkillHostID != "" || strings.TrimSpace(value.ExternalSkillTargets) != "" ||
			strings.TrimSpace(value.ExternalSkillRoots) != "" {
			return ProcessConfig{}, errors.New("server: external_skills JSON cannot be combined with split external Skill settings")
		}
		var external externalSkillsEnvironment
		if err := decodeJSONObject(value.ExternalSkills, &external); err != nil {
			return ProcessConfig{}, fmt.Errorf("server: external_skills must be a JSON object: %w", err)
		}
		externalHostID, targets, roots = external.HostID, external.Targets, external.CodexRoots
	} else {
		if strings.TrimSpace(value.ExternalSkillTargets) != "" {
			if err := decodeJSONArray(value.ExternalSkillTargets, &targets); err != nil {
				return ProcessConfig{}, fmt.Errorf("server: external_skills.targets must be a JSON array: %w", err)
			}
		}
		if strings.TrimSpace(value.ExternalSkillRoots) != "" {
			if err := decodeJSONArray(value.ExternalSkillRoots, &roots); err != nil {
				return ProcessConfig{}, fmt.Errorf("server: external_skills.codex_roots must be a JSON array: %w", err)
			}
		}
	}

	config := ProcessConfig{
		HTTP:      HTTPConfig{Host: value.HTTPHost, Port: value.HTTPPort},
		MCP:       MCPConfig{Enabled: value.MCPEnabled, Path: mcpPath},
		Auth:      AuthConfig{Enabled: value.AuthEnabled, Token: value.AuthToken},
		Dashboard: DashboardConfig{Enabled: value.DashboardEnabled, Scopes: scopes},
		Logging:   LoggingConfig{Level: strings.ToUpper(value.LoggingLevel), Format: value.LoggingFormat, Access: value.LoggingAccess},
		Metrics:   MetricsConfig{Enabled: value.MetricsEnabled}, Tracing: TracingConfig{Enabled: value.TracingEnabled},
		Runtime: RuntimeConfig{
			ScopeCacheSize: value.ScopeCacheSize, SourceWindowLimit: value.SourceWindowLimit,
			MemoryExtractionProfile: memory.ExtractionProfile(value.MemoryExtractionProfile),
			MemoryRerankEnabled:     value.MemoryRerankEnabled, MemoryRerankCandidateLimit: value.MemoryRerankCandidateLimit,
			SourceWindowInterval: sourceSchedule, ExperienceIncubationInterval: experienceSchedule,
		},
		Database: DatabaseConfig{
			Kind: value.DatabaseKind,
			SQLite: SQLiteDatabaseConfig{
				URL: value.DatabaseURL, BusyTimeout: time.Duration(value.DatabaseBusyTimeoutMS) * time.Millisecond,
				JournalMode: value.DatabaseJournalMode, ForeignKeys: value.DatabaseForeignKeys,
				MaxOpenConns: value.DatabaseMaxOpenConns,
				MaxIdleConns: value.DatabaseMaxIdleConns, ConnMaxLifetime: maxLifetime,
			},
			OceanBase: OceanBaseDatabaseConfig{
				URL: value.DatabaseURL, MaxOpenConns: value.DatabaseMaxOpenConns,
				MaxIdleConns: value.DatabaseMaxIdleConns, MaxLifetime: maxLifetime,
			},
			SeekDB: SeekDBDatabaseConfig{
				Path: seekDBPath, Database: value.DatabaseName,
				LibraryPath:  strings.TrimSpace(value.DatabaseLibraryPath),
				MaxOpenConns: value.DatabaseMaxOpenConns, MaxIdleConns: value.DatabaseMaxIdleConns,
				MaxLifetime: maxLifetime,
			},
		},
		HandoffReport: HandoffReportConfig{Enabled: value.HandoffReportEnabled},
		Inference: InferenceConfig{
			GenerationModel: strings.TrimSpace(value.GenerationModel), GenerationTimeout: generationTimeout,
			GenerationMaxRequests: value.GenerationMaxRequests, EmbeddingModel: strings.TrimSpace(value.EmbeddingModel),
			EmbeddingProfileID: strings.TrimSpace(value.EmbeddingProfileID), EmbeddingDimension: value.EmbeddingDimension,
			EmbeddingNormalization: strings.TrimSpace(value.EmbeddingNormalization), EmbeddingTimeout: embeddingTimeout,
			EmbeddingBatchSize: value.EmbeddingBatchSize,
		},
		ExternalSkills: ExternalSkillsConfig{HostID: externalHostID, Targets: targets, CodexRoots: roots},
		SchedulerPath:  value.SchedulerPath,
	}
	if err := config.Validate(); err != nil {
		return ProcessConfig{}, err
	}
	return config, nil
}

func (c ProcessConfig) Validate() error {
	if strings.TrimSpace(c.HTTP.Host) == "" || c.HTTP.Port < 1 || c.HTTP.Port > 65_535 {
		return errors.New("server: HTTP host and port are invalid")
	}
	if _, err := normalizeMCPPath(c.MCP.Path); err != nil {
		return err
	}
	if c.Auth.Enabled && c.Auth.Token == "" {
		return errors.New("server: bearer token is required when authentication is enabled")
	}
	if err := validateDashboard(c.Dashboard); err != nil {
		return err
	}
	switch c.Logging.Level {
	case "DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL":
	default:
		return errors.New("server: logging level is invalid")
	}
	if c.Logging.Format != "console" && c.Logging.Format != "json" {
		return errors.New("server: logging format must be console or json")
	}
	if c.Runtime.ScopeCacheSize < 1 || c.Runtime.SourceWindowLimit < 1 ||
		c.Runtime.MemoryRerankCandidateLimit < 1 || c.Runtime.MemoryRerankCandidateLimit > 100 {
		return errors.New("server: Runtime limits are invalid")
	}
	if _, err := memory.ExtractionInstructions(c.Runtime.MemoryExtractionProfile); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if c.Database.Kind != "sqlite" && c.Database.Kind != "oceanbase" && c.Database.Kind != "seekdb" {
		return errors.New("server: database kind must be sqlite, oceanbase, or seekdb")
	}
	if c.Database.Kind == "sqlite" {
		if _, err := SQLiteDSN(c.Database.SQLite.URL); err != nil {
			return err
		}
		if c.Database.SQLite.BusyTimeout < 0 || c.Database.SQLite.MaxOpenConns < 1 || c.Database.SQLite.MaxIdleConns < 0 {
			return errors.New("server: SQLite connection settings are invalid")
		}
		switch c.Database.SQLite.JournalMode {
		case "WAL", "DELETE", "MEMORY":
		default:
			return errors.New("server: SQLite journal mode is invalid")
		}
	} else if c.Database.Kind == "oceanbase" {
		if err := sqlstore.ValidateOceanBaseURL(c.Database.OceanBase.URL); err != nil {
			return fmt.Errorf("server: %w", err)
		}
	} else {
		if strings.TrimSpace(c.Database.SeekDB.Path) == "" || c.Database.SeekDB.Path != strings.TrimSpace(c.Database.SeekDB.Path) {
			return errors.New("server: embedded seekDB path must be a non-empty trimmed path")
		}
		if c.Database.SeekDB.Database != "test" {
			return errors.New("server: embedded seekDB database must be test")
		}
		if c.Database.SeekDB.MaxOpenConns < 1 || c.Database.SeekDB.MaxIdleConns < 0 ||
			c.Database.SeekDB.MaxIdleConns > c.Database.SeekDB.MaxOpenConns || c.Database.SeekDB.MaxLifetime < 0 {
			return errors.New("server: embedded seekDB connection pool limits are invalid")
		}
	}
	if c.Inference.GenerationTimeout <= 0 || c.Inference.GenerationMaxRequests < 1 || c.Inference.EmbeddingTimeout <= 0 || c.Inference.EmbeddingBatchSize < 1 {
		return errors.New("server: inference limits are invalid")
	}
	if c.Inference.EmbeddingNormalization != "none" && c.Inference.EmbeddingNormalization != "unit" {
		return errors.New("server: embedding normalization must be none or unit")
	}
	embeddingConfigured := []bool{c.Inference.EmbeddingModel != "", c.Inference.EmbeddingProfileID != "", c.Inference.EmbeddingDimension != 0}
	if (embeddingConfigured[0] || embeddingConfigured[1] || embeddingConfigured[2]) && !(embeddingConfigured[0] && embeddingConfigured[1] && embeddingConfigured[2]) {
		return errors.New("server: embedding model, profile ID, and dimension must be configured together")
	}
	if c.Inference.EmbeddingDimension < 0 || (c.Inference.EmbeddingModel != "" && c.Inference.EmbeddingDimension < 1) {
		return errors.New("server: embedding dimension must be positive")
	}
	if c.Runtime.MemoryRerankEnabled && c.Inference.GenerationModel == "" {
		return errors.New("server: Memory reranking requires a generation model")
	}
	if c.Runtime.SourceWindowInterval != nil && c.Inference.GenerationModel == "" {
		return errors.New("server: scheduled Source processing requires a generation model")
	}
	if c.Runtime.ExperienceIncubationInterval != nil && c.Inference.GenerationModel == "" {
		return errors.New("server: scheduled Experience incubation requires a generation model")
	}
	if err := validateExternalSkills(c.ExternalSkills); err != nil {
		return err
	}
	if (c.Runtime.SourceWindowInterval != nil || c.Runtime.ExperienceIncubationInterval != nil) && strings.TrimSpace(c.SchedulerPath) == "" {
		return errors.New("server: scheduler path is required when schedules are enabled")
	}
	return nil
}

func validateDashboard(config DashboardConfig) error {
	if len(config.Scopes) > 100 {
		return errors.New("server: Dashboard supports at most 100 scopes")
	}
	seen := make(map[string]struct{}, len(config.Scopes))
	for _, scope := range config.Scopes {
		id, name := strings.TrimSpace(scope.ScopeID), strings.TrimSpace(scope.DisplayName)
		if id == "" || name == "" || id != scope.ScopeID || name != scope.DisplayName || len([]rune(id)) > 255 || len([]rune(name)) > 80 {
			return errors.New("server: Dashboard scope values are invalid")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("server: Dashboard scope IDs must be unique")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateExternalSkills(config ExternalSkillsConfig) error {
	targetCount := len(config.Targets) + len(config.CodexRoots)
	if targetCount > 0 && (strings.TrimSpace(config.HostID) == "" || config.HostID != strings.TrimSpace(config.HostID)) {
		return errors.New("server: external Skill Agent targets require a trimmed host identity")
	}
	if len([]rune(config.HostID)) > 128 {
		return errors.New("server: external Skill host identity is too long")
	}
	seen := make(map[string]struct{}, targetCount)
	validateTarget := func(id, agentKind, installationScope, path string) error {
		if len(id) < 1 || len(id) > 64 || !externalSkillTargetIDPattern.MatchString(id) || path == "" {
			return errors.New("server: external Skill Agent target is incomplete")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("server: external Skill Agent target IDs must be unique")
		}
		seen[id] = struct{}{}
		switch agentKind {
		case "codex", "claude_code":
		default:
			return errors.New("server: external Skill Agent kind is invalid")
		}
		switch installationScope {
		case "user", "project", "plugin":
		default:
			return errors.New("server: external Skill installation scope is invalid")
		}
		return nil
	}
	for _, target := range config.Targets {
		if err := validateTarget(target.TargetID, target.AgentKind, target.InstallationScope, target.Path); err != nil {
			return err
		}
	}
	for _, root := range config.CodexRoots {
		if err := validateTarget(root.RootID, "codex", root.InstallationScope, root.Path); err != nil {
			return err
		}
	}
	return nil
}

func positiveSeconds(field, value string) (time.Duration, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("server: %s must be positive", field)
	}
	duration := time.Duration(parsed * float64(time.Second))
	if duration <= 0 || duration%time.Microsecond != 0 {
		return 0, fmt.Errorf("server: %s must have microsecond precision", field)
	}
	return duration, nil
}

func optionalPositiveSeconds(field, value string) (*time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	duration, err := positiveSeconds(field, value)
	if err != nil {
		return nil, err
	}
	return &duration, nil
}

func optionalDuration(field, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("server: %s is invalid", field)
	}
	return duration, nil
}

func decodeJSONArray(encoded string, target any) error {
	if !strings.HasPrefix(strings.TrimSpace(encoded), "[") {
		return errors.New("expected JSON array")
	}
	return decodeJSONValue(encoded, target)
}

func decodeJSONObject(encoded string, target any) error {
	if !strings.HasPrefix(strings.TrimSpace(encoded), "{") {
		return errors.New("expected JSON object")
	}
	return decodeJSONValue(encoded, target)
}

func decodeJSONValue(encoded string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(encoded))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

// PowerContextDataDir matches platformdirs.user_data_path("powercontext",
// appauthor=False) on the supported Linux and macOS release targets.
func PowerContextDataDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(PowerContextHomeEnv)); configured != "" {
		return absoluteExpandedPath(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("server: resolve user data directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "powercontext"), nil
	case "linux":
		if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
			return absoluteExpandedPath(filepath.Join(dataHome, "powercontext"))
		}
		return filepath.Join(home, ".local", "share", "powercontext"), nil
	default:
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("server: resolve user data directory: %w", err)
		}
		return filepath.Join(base, "powercontext"), nil
	}
}

func DefaultDatabasePath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "powercontext.db"), nil
}

func DefaultSeekDBPath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "seekdb"), nil
}

func DefaultSchedulerPath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "scheduler.db"), nil
}

func sqliteURL(path string) string {
	if path == ":memory:" {
		return "sqlite+aiosqlite:///:memory:"
	}
	return "sqlite+aiosqlite:///" + filepath.ToSlash(path)
}

// SQLiteDSN converts the frozen SQLAlchemy URL spelling without accepting a
// different database scheme by accident. Three-slash URLs intentionally keep
// a relative database path: the frozen .env.example relies on resolving that
// path from the Server working directory, just as SQLAlchemy does.
func SQLiteDSN(value string) (string, error) {
	const prefix = "sqlite+aiosqlite:///"
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("server: SQLite URL must use sqlite+aiosqlite")
	}
	database := strings.TrimPrefix(value, prefix)
	if database == "" {
		return "", errors.New("server: SQLite URL must identify a database")
	}
	if database == ":memory:" {
		return database, nil
	}
	return filepath.FromSlash(database), nil
}

func absoluteExpandedPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	current := absolute
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", resolveErr
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
