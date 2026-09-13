package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVSphereLocationList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"data":[{"id":"l-1","code":"ANX01","name":"Vienna","country_name":"Austria"}]}`)

	stdout, _, err := run(t, "vsphere", "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/location.json", last.path)
	require.Equal(t, "ID    CODE    NAME     COUNTRY\n"+
		"l-1   ANX01   Vienna   Austria\n", stdout)
}

func TestVSphereLocationListEscapesFilters(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Vienna & East", r.URL.Query().Get("location_code"))
		require.Equal(t, "org & team", r.URL.Query().Get("organization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "location", "list", "--code", "Vienna & East", "--organization", "org & team",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereTemplateList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[{"id":"t-1","name":"Ubuntu","build":"24.04","bit":"64"}]`)

	stdout, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/templates.json/l-1/templates", last.path)
	require.Equal(t, "ID    NAME     BUILD   BIT\n"+
		"t-1   Ubuntu   24.04   64\n", stdout)
}

func TestVSphereTemplateListRejectsInvalidType(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--type", "invalid", "--token", "tok")
	require.ErrorContains(t, err, `--type must be "templates" or "from_scratch"`)
}

func TestVSphereDiskTypeList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`[{"id":"d-1","storage_type":"ssd","bandwidth":100,"iops":200,"latency":3}]`)

	stdout, _, err := run(t, "vsphere", "disk-type", "list", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/disk_type.json/l-1", last.path)
	require.Equal(t, "ID    STORAGE TYPE   BANDWIDTH   IOPS   LATENCY\n"+
		"d-1   ssd            100         200    3\n", stdout)
}

func TestVSphereListsReportEmptyResults(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
		want string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[]}`, "no locations found\n"},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, `[]`, "no templates found\n"},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[]`, "no disk types found\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)
			_, stderr, err := run(t, append(tt.args, "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, tt.want, stderr)
		})
	}
}

func TestVSphereListsRenderEmptyJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[]}`},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, `[]`},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)
			stdout, _, err := run(t, append(tt.args, "-o", "json", "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, "[]\n", stdout)
		})
	}
}

func TestVSphereListsRejectInvalidPaging(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"location", []string{"vsphere", "location", "list"}},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			_, _, err := run(t, append(tt.args, "--page", "0", "--token", "tok")...)
			require.ErrorContains(t, err, "--page 0 must be 1 or greater")
			_, _, err = run(t, append(tt.args, "--limit", "0", "--token", "tok")...)
			require.ErrorContains(t, err, "--limit 0 must be between 1 and 1000")
		})
	}
}

func TestVSphereListsRequireLocation(t *testing.T) {
	for _, args := range [][]string{
		{"vsphere", "template", "list"},
		{"vsphere", "disk-type", "list"},
	} {
		t.Run(args[1], func(t *testing.T) {
			isolate(t)
			_, _, err := run(t, append(args, "--token", "tok")...)
			require.ErrorContains(t, err, "--location is required")
		})
	}
}

func TestVSphereListsWalkPages(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[{"id":"l-%d"}]}`},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, `[{"id":"t-%d"}]`},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[{"id":"d-%d"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			var seen []int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)
				seen = append(seen, page)
				w.Header().Set("Content-Type", "application/json")
				if page < 3 {
					_, _ = fmt.Fprintf(w, tt.body, page)
					return
				}
				if tt.name == "location" {
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			t.Cleanup(srv.Close)

			stdout, _, err := run(t, append(tt.args, "--all", "--limit", "1", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, []int{1, 2, 3}, seen)
			var got []struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got))
			require.Len(t, got, 2)
		})
	}
}
