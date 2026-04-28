package main

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "nodes", []string{"nodes"}},
		{"multiple", "nodes,pods,events", []string{"nodes", "pods", "events"}},
		{"with spaces", " nodes , pods ", []string{"nodes", "pods"}},
		{"trailing comma", "nodes,", []string{"nodes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTrimmed(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitTrimmed(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitTrimmed(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetEnvDefault(t *testing.T) {
	got := getEnvDefault("MCP_TEST_NONEXISTENT_VAR_XYZ", "fallback-val")
	if got != "fallback-val" {
		t.Errorf("getEnvDefault(unset) = %q, want %q", got, "fallback-val")
	}

	t.Setenv("MCP_TEST_HELPER_VAR", "  from-env  ")
	got = getEnvDefault("MCP_TEST_HELPER_VAR", "fallback")
	if got != "from-env" {
		t.Errorf("getEnvDefault(set) = %q, want %q", got, "from-env")
	}
}

func TestStringParam(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		param    string
		fallback string
		want     string
	}{
		{"missing param", map[string]interface{}{}, "sections", "default", "default"},
		{"empty string", map[string]interface{}{"sections": ""}, "sections", "default", "default"},
		{"whitespace only", map[string]interface{}{"sections": "  "}, "sections", "default", "default"},
		{"valid value", map[string]interface{}{"sections": "nodes,pods"}, "sections", "default", "nodes,pods"},
		{"trimmed", map[string]interface{}{"sections": " nodes "}, "sections", "default", "nodes"},
		{"non-string type", map[string]interface{}{"sections": 123}, "sections", "default", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args
			got := stringParam(req, tt.param, tt.fallback)
			if got != tt.want {
				t.Errorf("stringParam() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newDSCI(appsNS string) *unstructured.Unstructured {
	spec := map[string]interface{}{}
	if appsNS != "" {
		spec["applicationsNamespace"] = appsNS
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "dscinitialization.opendatahub.io/v2",
			"kind":       "DSCInitialization",
			"metadata":   map[string]interface{}{"name": "default-dsci"},
			"spec":       spec,
		},
	}
}

func fakeClient(dsci *unstructured.Unstructured) client.Client {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	b := fake.NewClientBuilder().WithScheme(s)
	if dsci != nil {
		b = b.WithRuntimeObjects(dsci)
	}
	return b.Build()
}

func TestDiscoverAppsNamespace(t *testing.T) {
	tests := []struct {
		name    string
		dsci    *unstructured.Unstructured
		client  client.Client
		env     string
		want    string
		wantErr bool
	}{
		{"DSCI with custom namespace", newDSCI("custom-apps"), nil, "", "custom-apps", false},
		{"DSCI with empty field", newDSCI(""), nil, "", defaultAppsNS, false},
		{"no DSCI falls back to default", nil, nil, "", defaultAppsNS, false},
		{"no DSCI uses env var", nil, nil, "env-apps", "env-apps", false},
		{"DSCI takes precedence over env", newDSCI("dsci-apps"), nil, "env-apps", "dsci-apps", false},
		{"RBAC forbidden ignores env var", nil, newForbiddenClient(), "env-apps", "", true},
		{"CRD not installed ignores env var", nil, newNoMatchClient(), "custom-apps", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envApplicationsNamespace, tt.env)
			c := tt.client
			if c == nil {
				c = fakeClient(tt.dsci)
			}
			got, err := discoverAppsNamespace(context.Background(), c)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverAppsNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("discoverAppsNamespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverOperatorNamespace(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"default", "", defaultOperatorNS},
		{"from env", "custom-operator-ns", "custom-operator-ns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envOperatorNamespace, tt.env)
			if got := discoverOperatorNamespace(); got != tt.want {
				t.Errorf("discoverOperatorNamespace() = %q, want %q", got, tt.want)
			}
		})
	}
}
