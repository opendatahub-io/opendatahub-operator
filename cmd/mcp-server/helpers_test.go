package main

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
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

func TestBoolParam(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		param    string
		fallback bool
		want     bool
	}{
		{"missing param", map[string]interface{}{}, "summary", false, false},
		{"true value", map[string]interface{}{"summary": true}, "summary", false, true},
		{"false value", map[string]interface{}{"summary": false}, "summary", true, false},
		{"non-bool type", map[string]interface{}{"summary": "yes"}, "summary", false, false},
		{"nil args", nil, "summary", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args
			got := boolParam(req, tt.param, tt.fallback)
			if got != tt.want {
				t.Errorf("boolParam() = %v, want %v", got, tt.want)
			}
		})
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
