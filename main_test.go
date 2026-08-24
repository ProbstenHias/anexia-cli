package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMainReportsClassifiedExitCodes exercises the real process boundary.
// Package tests cover classification and command behavior separately, but a
// hard-coded os.Exit(1) or a write to stdout in main would otherwise survive.
func TestMainReportsClassifiedExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantText string
	}{
		{name: "usage", args: []string{"bogus"}, wantCode: 2, wantText: "invalid usage:"},
		{name: "authentication", args: []string{"core", "location", "list"}, wantCode: 3, wantText: "not authenticated:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//nolint:gosec // G702: executable is this test binary; arguments are test constants.
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestMainHelperProcess", "--"}, tt.args...)...)
			cmd.Env = append(os.Environ(),
				"ANEXIA_TEST_MAIN_HELPER=1",
				"ANEXIA_CONFIG="+filepath.Join(t.TempDir(), "config.yaml"),
				"ANEXIA_TOKEN=",
			)

			stdout := new(strings.Builder)
			stderr := new(strings.Builder)
			cmd.Stdout = stdout
			cmd.Stderr = stderr

			err := cmd.Run()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			require.Equal(t, tt.wantCode, exitErr.ExitCode())
			require.Empty(t, stdout.String(), "errors belong on stderr")
			require.Contains(t, stderr.String(), "anexia: "+tt.wantText)
		})
	}
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("ANEXIA_TEST_MAIN_HELPER") != "1" {
		return
	}

	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	require.NotEqual(t, -1, separator)

	os.Args = append([]string{"anexia"}, os.Args[separator+1:]...)
	main()
}
