package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact/memory"
)

func TestLoadConfigMatchesFrozenServerEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv(PowerContextHomeEnv, home)
	t.Setenv("POWERCONTEXT_SERVER_EXTERNAL_SKILLS_HOST_ID", "")
	t.Setenv("POWERCONTEXT_SERVER_EXTERNAL_SKILLS_CODEX_ROOTS", "")
	t.Setenv("POWERCONTEXT_SERVER_HTTP_HOST", "127.0.0.2")
	t.Setenv("POWERCONTEXT_SERVER_HTTP_PORT", "9000")
	t.Setenv("POWERCONTEXT_SERVER_DATABASE_URL", "sqlite+aiosqlite:////var/lib/powercontext/test.db")
	t.Setenv("POWERCONTEXT_SERVER_RUNTIME_SOURCE_WINDOW_LIMIT", "25")
	t.Setenv("POWERCONTEXT_SERVER_RUNTIME_MEMORY_EXTRACTION_PROFILE", "conversation")
	t.Setenv("POWERCONTEXT_SERVER_RUNTIME_MEMORY_RERANK_ENABLED", "true")
	t.Setenv("POWERCONTEXT_SERVER_RUNTIME_MEMORY_RERANK_CANDIDATE_LIMIT", "40")
	t.Setenv("POWERCONTEXT_SERVER_RUNTIME_EXPERIENCE_SCHEDULE_SECONDS", "45")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_GENERATION_MODEL", " test ")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_GENERATION_TIMEOUT_SECONDS", "12.5")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_GENERATION_MAX_REQUESTS", "4")
	t.Setenv("POWERCONTEXT_SERVER_MCP_ENABLED", "false")
	t.Setenv("POWERCONTEXT_SERVER_MCP_PATH", "/context/")
	t.Setenv("POWERCONTEXT_SERVER_EXTERNAL_SKILLS", `{
		"host_id":"workstation-1",
		"future_compatible":true,
		"codex_roots":[{
			"root_id":"repository",
			"installation_scope":"project",
			"path":"/srv/project/.agents/skills",
			"future_compatible":true
		}]
	}`)

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := absoluteExpandedPath(home)
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTP.Host != "127.0.0.2" || config.HTTP.Port != 9000 {
		t.Fatalf("HTTP config = %#v", config.HTTP)
	}
	if config.Database.Kind != "sqlite" || config.Database.SQLite.URL != "sqlite+aiosqlite:////var/lib/powercontext/test.db" {
		t.Fatalf("database config = %#v", config.Database)
	}
	if config.Runtime.SourceWindowLimit != 25 || config.Runtime.MemoryExtractionProfile != memory.ConversationProfile ||
		!config.Runtime.MemoryRerankEnabled || config.Runtime.MemoryRerankCandidateLimit != 40 ||
		config.Runtime.ExperienceIncubationInterval == nil || *config.Runtime.ExperienceIncubationInterval != 45*time.Second {
		t.Fatalf("Runtime config = %#v", config.Runtime)
	}
	if config.Inference.GenerationModel != "test" || config.Inference.GenerationTimeout != 12500*time.Millisecond ||
		config.Inference.GenerationMaxRequests != 4 {
		t.Fatalf("generation config = %#v", config.Inference)
	}
	if config.MCP.Enabled || config.MCP.Path != "/context" {
		t.Fatalf("MCP config = %#v", config.MCP)
	}
	if config.ExternalSkills.HostID != "workstation-1" || len(config.ExternalSkills.Roots) != 1 {
		t.Fatalf("external Skill config = %#v", config.ExternalSkills)
	}
	root := config.ExternalSkills.Roots[0]
	if root.RootID != "repository" || root.InstallationScope != "project" || root.Path != "/srv/project/.agents/skills" {
		t.Fatalf("external Skill root = %#v", root)
	}
	if config.SchedulerPath != filepath.Join(resolvedHome, "scheduler.db") {
		t.Fatalf("scheduler path = %q", config.SchedulerPath)
	}
}

func TestDefaultConfigUsesPersistentUserStorage(t *testing.T) {
	home := filepath.Join(t.TempDir(), "powercontext-data")
	t.Setenv(PowerContextHomeEnv, home)

	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := absoluteExpandedPath(home)
	if err != nil {
		t.Fatal(err)
	}
	wantDatabase := sqliteURL(filepath.Join(resolvedHome, "powercontext.db"))
	if config.Database.Kind != "sqlite" || config.Database.SQLite.URL != wantDatabase {
		t.Fatalf("database config = %#v, want SQLite %q", config.Database, wantDatabase)
	}
	if config.SchedulerPath != filepath.Join(resolvedHome, "scheduler.db") {
		t.Fatalf("scheduler path = %q", config.SchedulerPath)
	}
}

func TestLoadConfigSelectsOceanBase(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	const databaseURL = "mysql+aoceanbase://root:test@127.0.0.1:2881/powercontext?charset=utf8mb4"
	t.Setenv("POWERCONTEXT_SERVER_DATABASE_KIND", "oceanbase")
	t.Setenv("POWERCONTEXT_SERVER_DATABASE_URL", databaseURL)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Database.Kind != "oceanbase" || config.Database.OceanBase.URL != databaseURL {
		t.Fatalf("database config = %#v", config.Database)
	}
}

func TestLoadConfigEmbeddingProfileAndDefaultsMatchPython(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_EMBEDDING_MODEL", " test-provider:test-model ")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_EMBEDDING_PROFILE_ID", " test-profile-v1 ")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_EMBEDDING_DIMENSION", "3")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_EMBEDDING_NORMALIZATION", " unit ")
	t.Setenv("POWERCONTEXT_SERVER_INFERENCE_EMBEDDING_BATCH_SIZE", "7")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Inference.EmbeddingModel != "test-provider:test-model" ||
		config.Inference.EmbeddingProfileID != "test-profile-v1" || config.Inference.EmbeddingDimension != 3 ||
		config.Inference.EmbeddingNormalization != "unit" || config.Inference.EmbeddingBatchSize != 7 {
		t.Fatalf("embedding config = %#v", config.Inference)
	}

	defaults, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Inference.EmbeddingNormalization != "unit" || defaults.Inference.EmbeddingBatchSize != 10 {
		t.Fatalf("embedding defaults = %#v", defaults.Inference)
	}
}

func TestProcessConfigRejectsInvalidAndPartialEmbeddingProfiles(t *testing.T) {
	for _, mutate := range []func(*ProcessConfig){
		func(config *ProcessConfig) { config.Inference.EmbeddingNormalization = "provider-default" },
		func(config *ProcessConfig) { config.Inference.EmbeddingModel = "provider:model" },
		func(config *ProcessConfig) {
			config.Inference.EmbeddingModel = "provider:model"
			config.Inference.EmbeddingProfileID = "profile-v1"
		},
	} {
		config, err := DefaultConfig()
		if err != nil {
			t.Fatal(err)
		}
		mutate(&config)
		if err := config.Validate(); err == nil {
			t.Fatalf("invalid embedding config was accepted: %#v", config.Inference)
		}
	}
}

func TestComponentConfigHasNoLegacySQLitePathSurface(t *testing.T) {
	t.Parallel()
	typeOfSQLite := reflect.TypeOf(SQLiteDatabaseConfig{})
	if _, found := typeOfSQLite.FieldByName("LegacyPath"); found {
		t.Fatal("SQLiteConfig exposes the removed legacy_path component setting")
	}
	if _, found := typeOfSQLite.FieldByName("Path"); found {
		t.Fatal("SQLiteConfig exposes a second path authority beside URL")
	}
}

func TestLoadConfigRejectsAmbiguousExternalSkillEnvironment(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	t.Setenv("POWERCONTEXT_SERVER_EXTERNAL_SKILLS", `{"host_id":"host","codex_roots":[]}`)
	t.Setenv("POWERCONTEXT_SERVER_EXTERNAL_SKILLS_HOST_ID", "other-host")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("mixed external Skill environment was accepted")
	}
}

func TestAuthenticationConfigurationRedactsTokenAcrossRepresentations(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	t.Setenv("POWERCONTEXT_SERVER_AUTH_ENABLED", "true")
	t.Setenv("POWERCONTEXT_SERVER_AUTH_TOKEN", "server-secret")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	auth := config.Auth
	if !auth.Enabled || auth.Token != "server-secret" {
		t.Fatalf("authentication config = %#v", auth)
	}
	for _, rendered := range []string{
		fmt.Sprintf("%v", auth),
		fmt.Sprintf("%+v", auth),
		fmt.Sprintf("%#v", auth),
		fmt.Sprintf("%v", ProcessConfig{Auth: auth}),
		fmt.Sprintf("%+v", ProcessConfig{Auth: auth}),
		fmt.Sprintf("%#v", ProcessConfig{Auth: auth}),
	} {
		if strings.Contains(rendered, auth.Token) {
			t.Fatalf("formatted Auth config leaked token: %s", rendered)
		}
	}
	encoded, err := json.Marshal(auth)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), auth.Token) || strings.Contains(string(encoded), "Token") {
		t.Fatalf("JSON Auth config leaked token: %s", encoded)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logger.Info("configured", slog.Any("auth", auth))
	if strings.Contains(output.String(), auth.Token) || !strings.Contains(output.String(), `"token_configured":true`) {
		t.Fatalf("structured Auth config was not safely represented: %s", output.String())
	}
}

func TestLoadConfigRequiresBearerTokenWhenEnabled(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	t.Setenv("POWERCONTEXT_SERVER_AUTH_ENABLED", "true")
	t.Setenv("POWERCONTEXT_SERVER_AUTH_TOKEN", "")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "bearer token is required") {
		t.Fatalf("authentication error = %v", err)
	}
}

func TestLoadConfigNormalizesLoggingSettings(t *testing.T) {
	t.Setenv(PowerContextHomeEnv, t.TempDir())
	t.Setenv("POWERCONTEXT_SERVER_LOGGING_LEVEL", "warning")
	t.Setenv("POWERCONTEXT_SERVER_LOGGING_FORMAT", "json")
	t.Setenv("POWERCONTEXT_SERVER_LOGGING_ACCESS", "false")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Logging != (LoggingConfig{Level: "WARNING", Format: "json", Access: false}) {
		t.Fatalf("logging config = %#v", config.Logging)
	}
}

func TestProcessConfigEnforcesTrustAndInferenceBoundariesWithoutSecrets(t *testing.T) {
	t.Parallel()
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Database.SQLite.URL = sqliteURL(filepath.Join(t.TempDir(), "powercontext.db"))
	config.Dashboard.Enabled = true
	config.Dashboard.Scopes = []DashboardScope{{ScopeID: "scope", DisplayName: "Scope"}}
	if err := config.Validate(); err == nil {
		t.Fatal("Dashboard without authentication was accepted")
	}
	config.Auth = AuthConfig{Enabled: true, Token: "secret"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Inference.EmbeddingModel = "openai:text-embedding-3-small"
	if err := config.Validate(); err == nil {
		t.Fatal("partial embedding profile was accepted")
	}
}

func TestScheduledExperienceIncubationRequiresGenerationModel(t *testing.T) {
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	interval := time.Minute
	config.Runtime.ExperienceIncubationInterval = &interval
	config.Inference.GenerationModel = ""
	err = config.Validate()
	if err == nil || !strings.Contains(err.Error(), "scheduled Experience incubation requires a generation model") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestSQLiteDSNRequiresFrozenDialectAndAbsolutePath(t *testing.T) {
	t.Parallel()
	want := filepath.Join(t.TempDir(), "powercontext.db")
	got, err := SQLiteDSN(sqliteURL(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DSN = %q, want %q", got, want)
	}
	for _, invalid := range []string{"sqlite:///tmp/a.db", "sqlite+aiosqlite:///relative.db", ""} {
		if _, err := SQLiteDSN(invalid); err == nil {
			t.Fatalf("accepted invalid SQLite URL %q", invalid)
		}
	}
}

func TestProcessConfigValidatesOceanBaseURLWithoutLeakingPassword(t *testing.T) {
	t.Parallel()
	config, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Database.Kind = "oceanbase"
	config.Database.OceanBase.URL = "mysql+pymysql://root:do-not-leak@127.0.0.1:2881/powercontext?charset=utf8mb4"
	err = config.Validate()
	if err == nil {
		t.Fatal("non-official OceanBase URL was accepted")
	}
	if strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("configuration error leaked password: %v", err)
	}
	config.Database.OceanBase.URL = "mysql+aoceanbase://root%40tenant:secret@127.0.0.1:2881/powercontext?charset=utf8mb4"
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}
