package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/config"
)

func TestPathExplicitWins(t *testing.T) {
	t.Setenv("ANEXIA_CONFIG", "/env/config.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	got, err := config.Path("/explicit/config.yaml")
	require.NoError(t, err)
	require.Equal(t, "/explicit/config.yaml", got)
}

func TestPathFromEnvVar(t *testing.T) {
	t.Setenv("ANEXIA_CONFIG", "/env/config.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	got, err := config.Path("")
	require.NoError(t, err)
	require.Equal(t, "/env/config.yaml", got)
}

func TestPathFromXDG(t *testing.T) {
	t.Setenv("ANEXIA_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	got, err := config.Path("")
	require.NoError(t, err)
	require.Equal(t, "/xdg/anexia/config.yaml", got)
}

func TestPathFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANEXIA_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := config.Path("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".config", "anexia", "config.yaml"), got)
}

func TestLoadMissingFileReturnsZeroConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.Config{}, cfg)
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anexia", "config.yaml")
	want := config.Config{Token: "secret-token", APIBaseURL: "https://engine.example.com"}

	require.NoError(t, config.Save(path, want))

	got, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anexia", "config.yaml")

	require.NoError(t, config.Save(path, config.Config{Token: "t"}))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	di, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	require.NoError(t, config.Save(path, config.Config{Token: "first"}))
	require.NoError(t, config.Save(path, config.Config{Token: "second"}))

	got, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.Config{Token: "second"}, got)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "temporary files must not be left behind")
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("token: t\nnope: 1\n"), 0o600))

	_, err := config.Load(path)
	require.ErrorContains(t, err, `unknown config key "nope"`)
	require.ErrorContains(t, err, path)
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("token: [unclosed\n"), 0o600))

	_, err := config.Load(path)
	require.ErrorContains(t, err, path)
}

func TestSetAndGet(t *testing.T) {
	t.Parallel()

	var cfg config.Config

	require.NoError(t, cfg.Set("token", "abcdefghij"))
	require.NoError(t, cfg.Set("api_base_url", "https://engine.example.com"))
	require.Equal(t, "abcdefghij", cfg.Token)
	require.Equal(t, "https://engine.example.com", cfg.APIBaseURL)

	got, err := cfg.Get("api_base_url")
	require.NoError(t, err)
	require.Equal(t, "https://engine.example.com", got)
}

func TestGetTokenIsMasked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Token: "abcdefghij"}

	got, err := cfg.Get("token")
	require.NoError(t, err)
	require.Equal(t, "******ghij", got)
}

func TestMask(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", config.Mask(""))
	require.Equal(t, "****", config.Mask("abc"))
	require.Equal(t, "****", config.Mask("abcd"))
	require.Equal(t, "*bcde", config.Mask("abcde"))
}

func TestSetUnknownKey(t *testing.T) {
	t.Parallel()

	var cfg config.Config

	err := cfg.Set("nope", "x")
	require.ErrorContains(t, err, `unknown config key "nope"`)
	require.ErrorContains(t, err, "token, api_base_url")
}

func TestGetUnknownKey(t *testing.T) {
	t.Parallel()

	_, err := config.Config{}.Get("nope")
	require.ErrorContains(t, err, `unknown config key "nope"`)
}

func TestRedacted(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Token: "abcdefghij", APIBaseURL: "https://engine.example.com"}
	require.Equal(t, config.Config{Token: "******ghij", APIBaseURL: "https://engine.example.com"}, cfg.Redacted())
	require.Equal(t, "abcdefghij", cfg.Token, "Redacted must not mutate the receiver")
}
