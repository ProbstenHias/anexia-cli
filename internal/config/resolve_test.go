package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/config"
)

func newFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("token", "", "api token")
	fs.String("api-base-url", "", "api base url")
	fs.String("output", "table", "output format")
	require.NoError(t, fs.Parse(args))

	return fs
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		environ []string
		args    []string
		want    config.Config
	}{
		{
			name: "file only",
			file: "token: file-token\napi_base_url: https://file.example.com\n",
			want: config.Config{Token: "file-token", APIBaseURL: "https://file.example.com"},
		},
		{
			name:    "env overrides file",
			file:    "token: file-token\napi_base_url: https://file.example.com\n",
			environ: []string{"ANEXIA_TOKEN=env-token", "ANEXIA_API_BASE_URL=https://env.example.com"},
			want:    config.Config{Token: "env-token", APIBaseURL: "https://env.example.com"},
		},
		{
			name:    "changed flag overrides env and file",
			file:    "token: file-token\napi_base_url: https://file.example.com\n",
			environ: []string{"ANEXIA_TOKEN=env-token", "ANEXIA_API_BASE_URL=https://env.example.com"},
			args:    []string{"--token", "flag-token", "--api-base-url", "https://flag.example.com"},
			want:    config.Config{Token: "flag-token", APIBaseURL: "https://flag.example.com"},
		},
		{
			name: "unchanged flag default does not clobber file",
			file: "token: file-token\napi_base_url: https://file.example.com\n",
			args: []string{"--output", "json"},
			want: config.Config{Token: "file-token", APIBaseURL: "https://file.example.com"},
		},
		{
			name:    "env token with file base url",
			file:    "api_base_url: https://file.example.com\n",
			environ: []string{"ANEXIA_TOKEN=env-token"},
			want:    config.Config{Token: "env-token", APIBaseURL: "https://file.example.com"},
		},
		{
			name:    "flag token with env base url",
			environ: []string{"ANEXIA_API_BASE_URL=https://env.example.com"},
			args:    []string{"--token", "flag-token"},
			want:    config.Config{Token: "flag-token", APIBaseURL: "https://env.example.com"},
		},
		{
			name:    "empty env values are ignored",
			file:    "token: file-token\n",
			environ: []string{"ANEXIA_TOKEN=", "ANEXIA_API_BASE_URL="},
			want:    config.Config{Token: "file-token"},
		},
		{
			name:    "unrelated env vars are ignored",
			environ: []string{"PATH=/usr/bin", "ANEXIA_SOMETHING=x"},
			args:    []string{"--token", "flag-token"},
			want:    config.Config{Token: "flag-token"},
		},
		{
			name: "no sources yields zero config",
			want: config.Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "missing.yaml")
			if tt.file != "" {
				path = writeConfig(t, tt.file)
			}

			environ := tt.environ
			got, err := config.Resolve(path, newFlags(t, tt.args...), func() []string { return environ })
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolvePropagatesLoadError(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "nope: 1\n")

	_, err := config.Resolve(path, newFlags(t), func() []string { return nil })
	require.ErrorContains(t, err, `unknown config key "nope"`)
}

func TestResolveNilFlagSet(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "token: file-token\n")

	got, err := config.Resolve(path, nil, func() []string { return nil })
	require.NoError(t, err)
	require.Equal(t, config.Config{Token: "file-token"}, got)
}
