package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

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

// TestLoadKeepsTokensThatLookLikeOtherTypes covers a hand-written config file,
// which the README invites by documenting the format. YAML resolves an unquoted
// scalar by its own type rules, so a token made only of digits is a number by
// the time it reaches the struct, and a long one loses digits to float64. The
// user then sees a masked value that looks right while every request fails on
// authentication, with nothing pointing at the file.
func TestLoadKeepsTokensThatLookLikeOtherTypes(t *testing.T) {
	tokens := []string{
		"12345678901234567890123456789012",
		"01234567890123456789012345678901",
		"0755",
		"0x1f",
		"1e5",
		"1_000",
		"2024-01-02",
		"true",
		".inf",
	}

	for _, token := range tokens {
		t.Run(token, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("token: "+token+"\n"), 0o600))

			cfg, err := config.Load(path)
			require.NoError(t, err)
			require.Equal(t, token, cfg.Token)
		})
	}
}

// TestLoadRejectsNullLikeTokens covers YAML values that mean null rather than
// text. Silently decoding one to the empty string clears credentials; users who
// genuinely have such a token must quote it to make it a string.
func TestLoadRejectsNullLikeTokens(t *testing.T) {
	for _, token := range []string{"null", "Null", "NULL", "~"} {
		t.Run(token, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("token: "+token+"\n"), 0o600))

			_, err := config.Load(path)

			require.ErrorContains(t, err, `config key "token" is null`)
			require.ErrorContains(t, err, "quote it")
		})
	}
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

// TestMaskKeepsValidUTF8 covers a value that is not plain ASCII. Masking by
// byte splits a multi-byte rune and emits a lone continuation byte, so the
// output of the one function whose job is producing a safe string is not text.
// That reaches stdout through "config view" and breaks a consumer of -o json.
func TestMaskKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"äöüß1":     "*öüß1",
		"abcdefäöü": "*****fäöü",
		"日本語文字列":    "**語文字列",
		"🔑🔑🔑🔑🔑":     "*🔑🔑🔑🔑",
	}

	for value, want := range tests {
		masked := config.Mask(value)

		require.True(t, utf8.ValidString(masked), "Mask(%q) = %q is not valid UTF-8", value, masked)
		require.Equal(t, want, masked, "Mask(%q) must hide every rune except the last four", value)
	}
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
