package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

func TestKubernetesHelpListsResourcesAndVerbs(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "kubernetes")
	require.NoError(t, err)
	require.Contains(t, stdout, "cluster")
	require.Contains(t, stdout, "node-pool")

	tests := []struct {
		name string
		args []string
	}{
		{name: "cluster", args: []string{"kubernetes", "cluster"}},
		{name: "node-pool", args: []string{"kubernetes", "node-pool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := run(t, tt.args...)
			require.NoError(t, err)
			for _, verb := range []string{"list", "get", "create", "delete"} {
				require.Contains(t, stdout, verb)
			}
			require.NotContains(t, stdout, "update")
		})
	}
}

func TestKubernetesClusterCreateFlags(t *testing.T) {
	tests := []struct {
		name        string
		flags       []string
		wantError   string
		wantFields  map[string]any
		absentField string
	}{
		{name: "missing name", flags: []string{"--location", "l-1"}, wantError: "--name is required"},
		{name: "missing location", flags: []string{"--name", "demo"}, wantError: "--location is required"},
		{
			name: "payload",
			flags: []string{
				"--name", "demo", "--location", "l-1", "--version", "1.29",
				"--needs-service-vms=false", "--enable-nat-gateways", "--enable-lbaas=false", "--enable-autoscaling",
				"--internal-ipv4-prefix", "p-in", "--external-ipv4-prefix", "p-out", "--external-ipv6-prefix", "p-v6",
				"--api-server-allowlist", "10.0.0.0/8 192.0.2.0/24",
			},
			wantFields: map[string]any{
				"name":                        "demo",
				"location":                    "l-1",
				"version":                     "1.29",
				"needs_service_vms":           false,
				"enable_nat_gateways":         true,
				"enable_lbaas":                false,
				"autoscaling":                 true,
				"internal_ipv4_prefix":        "p-in",
				"manage_internal_ipv4_prefix": false,
				"external_ipv4_prefix":        "p-out",
				"manage_external_ipv4_prefix": false,
				"external_ipv6_prefix":        "p-v6",
				"manage_external_ipv6_prefix": false,
				"apiserver_allowlist":         "10.0.0.0/8 192.0.2.0/24",
			},
		},
		{
			name:        "engine defaults for omitted booleans",
			flags:       []string{"--name", "demo", "--location", "l-1"},
			absentField: "needs_service_vms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, `{"identifier":"c-1","name":"demo"}`)
			args := append([]string{"kubernetes", "cluster", "create"}, tt.flags...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)
			_, _, err := run(t, args...)
			if tt.wantError != "" {
				require.Error(t, err)
				require.Contains(t, errmap.Message(err), tt.wantError)
				require.Empty(t, last.method)
				return
			}

			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(last.body), &body))
			for field, want := range tt.wantFields {
				require.Equal(t, want, body[field], "field %s: got %v, want %v", field, body[field], want)
			}
			if tt.absentField != "" {
				_, present := body[tt.absentField]
				require.False(t, present, "field %s: got present, want absent", tt.absentField)
			}
		})
	}
}

func TestKubernetesNodePoolCreateFlags(t *testing.T) {
	base := []string{"--name", "workers", "--cluster", "c-1", "--cpus", "4", "--memory", "4", "--disk", "20"}
	tests := []struct {
		name      string
		flags     []string
		wantError string
		want      map[string]any
	}{
		{name: "missing name", flags: withoutFlag(base, "--name"), wantError: "--name is required"},
		{name: "missing cluster", flags: withoutFlag(base, "--cluster"), wantError: "--cluster is required"},
		{name: "zero cpus", flags: replaceFlag(base, "--cpus", "0"), wantError: "--cpus must be greater than zero"},
		{name: "zero memory", flags: replaceFlag(base, "--memory", "0"), wantError: "--memory must be greater than zero"},
		{name: "zero disk", flags: replaceFlag(base, "--disk", "0"), wantError: "--disk must be greater than zero"},
		{
			name:  "payload and default operating system",
			flags: append(append([]string{}, base...), "--replicas", "3"),
			want: map[string]any{
				"name":             "workers",
				"cluster":          "c-1",
				"cpus":             float64(4),
				"memory":           float64(4 << 30),
				"disk_size":        float64(20 << 30),
				"replicas":         float64(3),
				"operating_system": "Flatcar Linux",
			},
		},
		{
			name:  "replicas omitted for engine default",
			flags: base,
			want:  map[string]any{"operating_system": "Flatcar Linux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, `{"identifier":"np-1","name":"workers"}`)
			args := append([]string{"kubernetes", "node-pool", "create"}, tt.flags...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)
			_, _, err := run(t, args...)
			if tt.wantError != "" {
				require.Error(t, err)
				require.Contains(t, errmap.Message(err), tt.wantError)
				require.Empty(t, last.method)
				return
			}

			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(last.body), &body))
			for field, want := range tt.want {
				require.Equal(t, want, body[field], "field %s: got %v, want %v", field, body[field], want)
			}
			if tt.name == "replicas omitted for engine default" {
				_, present := body["replicas"]
				require.False(t, present, "field replicas: got present, want absent")
			}
		})
	}
}

func TestKubernetesListColumns(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
		want string
	}{
		{
			name: "cluster location and state fallbacks",
			args: []string{"kubernetes", "cluster", "list"},
			body: `{"data":{"data":[{"identifier":"c-1","name":"demo","version":"1.29","location":{"code":"ANX04"},"state":{"title":"Running"}},{"identifier":"c-2","name":"other","location":{"name":"Vienna"},"state":{"id":"pending"}},{"identifier":"c-3","location":{"identifier":"l-3"}}]}}`,
			want: "c-1\tdemo\t1.29\tANX04\tRunning\nc-2\tother\t\tVienna\tpending\nc-3\t\t\tl-3\t\n",
		},
		{
			name: "node pool values",
			args: []string{"kubernetes", "node-pool", "list"},
			body: `{"data":{"data":[{"identifier":"np-1","name":"workers","cluster":{"name":"demo"},"replicas":3,"cpus":4,"memory":4294967296,"disk_size":21474836480,"state":{"title":"Running"}},{"identifier":"np-2","cluster":{"identifier":"c-2"},"memory":2147483648,"disk_size":21474836480,"state":{"id":"pending"}}]}}`,
			want: "np-1\tworkers\tdemo\t3\t4\t4Gi\t20Gi\tRunning\nnp-2\t\tc-2\t\t0\t2Gi\t20Gi\tpending\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)
			args := append(append([]string{}, tt.args...), "-o", "tsv", "--no-headers", "--token", "tok", "--api-base-url", srv.URL)
			stdout, _, err := run(t, args...)
			require.NoError(t, err)
			require.Equal(t, tt.want, stdout, "got %q, want %q", stdout, tt.want)
		})
	}
}

func TestKubernetesNodePoolClusterFilter(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{"data":{"data":[]}}`)

	_, _, err := run(t, "kubernetes", "node-pools", "list", "--cluster", "c-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "filters=")
	require.Contains(t, last.query, "cluster")
	require.Contains(t, last.query, "c-1")
}

func TestKubernetesKubeconfigOperations(t *testing.T) {
	tests := []struct {
		name       string
		responses  []string
		input      string
		args       []string
		wantOutput string
		wantPath   string
		wantMethod string
	}{
		{
			name:       "existing config",
			responses:  []string{`{"identifier":"c-1","kubeconfig":"apiVersion: v1\n"}`},
			args:       []string{"kubernetes", "cluster", "kubeconfig", "get", "c-1"},
			wantOutput: "apiVersion: v1\n",
		},
		{
			name:       "request rule then poll",
			responses:  []string{`{"identifier":"c-1"}`, `{}`, `{"identifier":"c-1","kubeconfig":"config"}`},
			args:       []string{"kubernetes", "cluster", "kubeconfig", "get", "c-1", "--timeout", "7s"},
			wantOutput: "config",
			wantPath:   "/api/kubernetes/v1/cluster.json/c-1/rule/12277a581e1c47cba72338425a008aa3",
			wantMethod: http.MethodPost,
		},
		{
			name:       "delete rule",
			responses:  []string{`{}`},
			input:      "y\n",
			args:       []string{"kubernetes", "cluster", "kubeconfig", "delete", "c-1"},
			wantPath:   "/api/kubernetes/v1/cluster.json/c-1/rule/eec87131729e44fa91a4b7ee8c365a26",
			wantMethod: http.MethodPost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			var mu sync.Mutex
			var seen []request
			index := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var body []byte
				if r.Body != nil {
					body, _ = io.ReadAll(r.Body)
				}
				seen = append(seen, request{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body)})
				response := tt.responses[index]
				index++
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, response)
			}))
			t.Cleanup(srv.Close)

			args := append(append([]string{}, tt.args...), "--token", "tok", "--api-base-url", srv.URL)
			var stdout, stderr string
			var err error
			if tt.input != "" {
				stdout, stderr, err = runWithInput(t, tt.input, args...)
			} else {
				stdout, stderr, err = run(t, args...)
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantOutput, stdout, "got %q, want %q", stdout, tt.wantOutput)
			if tt.wantPath != "" {
				require.Len(t, seen, len(tt.responses))
				found := false
				for _, request := range seen {
					if request.path == tt.wantPath && request.method == tt.wantMethod {
						found = true
						break
					}
				}
				require.True(t, found, "got requests %v, want %s %s", seen, tt.wantMethod, tt.wantPath)
			}
			_ = stderr
		})
	}
}

func TestKubernetesKubeconfigDeleteRespectsConfirmation(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{}`)

	_, _, err := runWithInput(t, "n\n", "kubernetes", "cluster", "kubeconfig", "delete", "c-1", "--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Empty(t, last.method, "got request %s, want no request", last.method)
}

func TestKubernetesEngineErrorPrefixes(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		prefix string
	}{
		{name: "cluster list", args: []string{"kubernetes", "cluster", "list"}, prefix: "listing clusters"},
		{name: "cluster get", args: []string{"kubernetes", "cluster", "get", "placeholder"}, prefix: `reading cluster "placeholder"`},
		{name: "cluster create", args: []string{"kubernetes", "cluster", "create", "--name", "placeholder", "--location", "placeholder"}, prefix: `creating cluster "placeholder"`},
		{name: "cluster delete", args: []string{"kubernetes", "cluster", "delete", "placeholder", "--yes"}, prefix: `deleting cluster "placeholder"`},
		{name: "kubeconfig get", args: []string{"kubernetes", "cluster", "kubeconfig", "get", "placeholder"}, prefix: `reading kubeconfig of cluster "placeholder"`},
		{name: "kubeconfig delete", args: []string{"kubernetes", "cluster", "kubeconfig", "delete", "placeholder", "--yes"}, prefix: `deleting kubeconfig of cluster "placeholder"`},
		{name: "node pool list", args: []string{"kubernetes", "node-pool", "list"}, prefix: "listing node-pools"},
		{name: "node pool get", args: []string{"kubernetes", "node-pool", "get", "placeholder"}, prefix: `reading node-pool "placeholder"`},
		{name: "node pool create", args: []string{"kubernetes", "node-pool", "create", "--name", "placeholder", "--cluster", "placeholder", "--cpus", "1", "--memory", "2", "--disk", "20"}, prefix: `creating node-pool "placeholder"`},
		{name: "node pool delete", args: []string{"kubernetes", "node-pool", "delete", "placeholder", "--yes"}, prefix: `deleting node-pool "placeholder"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusInternalServerError, `{"error":{"code":500,"message":"boom"}}`)
			args := append(append([]string{}, tt.args...), "--token", "tok", "--api-base-url", srv.URL)
			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Contains(t, errmap.Message(err), tt.prefix, "got %q, want prefix %q", errmap.Message(err), tt.prefix)
		})
	}
}

func withoutFlag(flags []string, name string) []string {
	result := make([]string, 0, len(flags))
	for i := 0; i < len(flags); i++ {
		if flags[i] == name {
			i++
			continue
		}
		result = append(result, flags[i])
	}
	return result
}

func replaceFlag(flags []string, name, value string) []string {
	result := append([]string{}, flags...)
	for i := range result {
		if result[i] == name && i+1 < len(result) {
			result[i+1] = value
		}
	}
	return result
}
