package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/stretchr/testify/require"
)

func TestLoadOrGenerate_GeneratesFileWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := LoadOrGenerate(path)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "0.0.0.0", cfg.Server.Host)
	require.Equal(t, "./jobs.db", cfg.Database.Path)
	require.NotEmpty(t, cfg.Scanner.RootPath)
	require.NotEmpty(t, cfg.Conversion.Codec)

	// The file was generated and round-trips through the strict loader.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "Auto-generated")

	reloaded, err := LoadMasterConfig(path)
	require.NoError(t, err)
	require.Equal(t, cfg.Server.Port, reloaded.Server.Port)
	require.Equal(t, cfg.Server.Host, reloaded.Server.Host)
	require.Equal(t, cfg.Database.Path, reloaded.Database.Path)
	require.Equal(t, cfg.Logging.Level, reloaded.Logging.Level)
}

func TestLoadOrGenerate_LoadsExistingFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "server:\n  port: 9090\n  host: 127.0.0.1\nscanner:\n  root_path: /data/videos\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadOrGenerate(path)
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, "/data/videos", cfg.Scanner.RootPath)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(data), "existing config file must not be rewritten")
}

func TestLoadOrGenerate_EmptyFileIsGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("  \n"), 0o600))

	cfg, err := LoadOrGenerate(path)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Server.Port)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "port: 8080")
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("DARKSTREAM_SERVER_HOST", "10.0.0.5")
	t.Setenv("DARKSTREAM_SERVER_PORT", "9999")
	t.Setenv("DARKSTREAM_API_KEY", "env-secret")
	t.Setenv("DARKSTREAM_TLS_CERT", "/certs/c.pem")
	t.Setenv("DARKSTREAM_TLS_KEY", "/certs/k.pem")
	t.Setenv("DARKSTREAM_DB_PATH", "/var/lib/darkstream/jobs.db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")

	cfg := models.DefaultMasterConfig()
	ApplyEnvOverrides(cfg)

	require.Equal(t, "10.0.0.5", cfg.Server.Host)
	require.Equal(t, 9999, cfg.Server.Port)
	require.Equal(t, "env-secret", cfg.Server.APIKey)
	require.Equal(t, "/certs/c.pem", cfg.Server.TLSCert)
	require.Equal(t, "/certs/k.pem", cfg.Server.TLSKey)
	require.Equal(t, "/var/lib/darkstream/jobs.db", cfg.Database.Path)
	require.Equal(t, "debug", cfg.Logging.Level)
	require.Equal(t, "text", cfg.Logging.Format)
}

func TestApplyEnvOverrides_InvalidPortIgnored(t *testing.T) {
	t.Setenv("DARKSTREAM_SERVER_PORT", "not-a-number")
	cfg := models.DefaultMasterConfig()
	ApplyEnvOverrides(cfg)
	require.Equal(t, 8080, cfg.Server.Port)
}

func TestPersisterUpdate_PreservesOtherSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	initial := `server:
  port: 8080
  host: 0.0.0.0
scanner:
  root_path: /data
  scan_interval: 5m
conversion:
  codec: h264
  preset: slow
`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))

	p := NewPersister(path)
	err := p.Update(ServerSettings{
		Host:         "127.0.0.1",
		Port:         9090,
		APIKey:       "new-key",
		DatabasePath: "./master.db",
		LogLevel:     "warn",
		LogFormat:    "text",
	})
	require.NoError(t, err)

	cfg, err := LoadMasterConfig(path)
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, "127.0.0.1", cfg.Server.Host)
	require.Equal(t, "new-key", cfg.Server.APIKey)
	require.Equal(t, "./master.db", cfg.Database.Path)
	require.Equal(t, "warn", cfg.Logging.Level)
	require.Equal(t, "text", cfg.Logging.Format)
	// Untouched sections survive the update.
	require.Equal(t, "/data", cfg.Scanner.RootPath)
	require.Equal(t, "h264", cfg.Conversion.Codec)
	require.Equal(t, "slow", cfg.Conversion.Preset)
}

func TestPersisterUpdate_CreatesFileWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	p := NewPersister(path)

	err := p.Update(ServerSettings{
		Host:         "0.0.0.0",
		Port:         8080,
		DatabasePath: "./jobs.db",
		LogLevel:     "info",
		LogFormat:    "json",
	})
	require.NoError(t, err)

	cfg, err := LoadMasterConfig(path)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "./jobs.db", cfg.Database.Path)

	// The file may contain the API key, so it must not be world-readable.
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
