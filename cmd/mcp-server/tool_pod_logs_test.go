package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// newTestClientset creates a clientset backed by the given HTTP handler.
func newTestClientset(t *testing.T, h http.Handler) kubernetes.Interface {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: ts.URL})
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}
	return cs
}

func TestPodLogs(t *testing.T) {
	echoHandler := func(msg string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, msg) }
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		args    map[string]interface{}
		want    string
		wantErr bool
	}{
		{"default args", echoHandler("line1\nline2\n"),
			map[string]interface{}{"pod_name": "my-pod", "namespace": "default"},
			"line1\nline2\n", false},
		{"with container", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("container") == "sidecar" {
				fmt.Fprint(w, "sidecar logs")
				return
			}
			http.NotFound(w, r)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default", "container": "sidecar"},
			"sidecar logs", false},
		{"previous logs", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("previous") == "true" {
				fmt.Fprint(w, "previous logs")
				return
			}
			http.NotFound(w, r)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default", "previous": true},
			"previous logs", false},
		{"custom tail_lines", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("tailLines") == "10" {
				fmt.Fprint(w, "10 lines")
				return
			}
			http.NotFound(w, r)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default", "tail_lines": float64(10)},
			"10 lines", false},
		{"default tail_lines", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("tailLines") == "100" {
				fmt.Fprint(w, "default 100 lines")
				return
			}
			http.NotFound(w, r)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default"},
			"default 100 lines", false},
		{"limit bytes set", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("limitBytes") == "51201" {
				fmt.Fprint(w, "limited output")
				return
			}
			http.NotFound(w, r)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default"},
			"limited output", false},
		{"missing pod_name", echoHandler(""),
			map[string]interface{}{"namespace": "default"},
			"pod_name is required", true},
		{"missing namespace", echoHandler(""),
			map[string]interface{}{"pod_name": "my-pod"},
			"namespace is required", true},
		{"pod not found", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods \"missing\" not found","reason":"NotFound","code":404}`)
		}, map[string]interface{}{"pod_name": "missing", "namespace": "default"},
			"not found in namespace", true},
		{"container not found", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"container \"bad\" is not valid for pod \"my-pod\"","reason":"BadRequest","code":400}`)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default", "container": "bad"},
			"container not found in pod", true},
		{"no previous logs", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"previous terminated container \"main\" in pod \"my-pod\" not found","reason":"BadRequest","code":400}`)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default", "previous": true},
			"no previous logs available", true},
		{"truncation", func(w http.ResponseWriter, r *http.Request) {
			// Send more than maxLogBytes (50KB) to trigger truncation.
			w.Write(make([]byte, maxLogBytes+100))
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "default"},
			"[truncated: output exceeded 50KB limit]", false},
		{"RBAC forbidden", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods \"my-pod\" is forbidden: User \"system:serviceaccount:test:default\" cannot get resource \"pods/log\" in API group \"\" in the namespace \"secure-ns\"","reason":"Forbidden","code":403}`)
		}, map[string]interface{}{"pod_name": "my-pod", "namespace": "secure-ns"},
			"RBAC insufficient", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := newTestClientset(t, tt.handler)
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := fetchPodLogs(context.Background(), cs, req)
			if err != nil {
				t.Fatalf("fetchPodLogs error: %v", err)
			}
			if result.IsError != tt.wantErr {
				t.Fatalf("IsError = %v, want %v", result.IsError, tt.wantErr)
			}
			if len(result.Content) == 0 {
				t.Fatalf("result.Content is empty")
			}
			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("result.Content[0] type = %T, want mcp.TextContent", result.Content[0])
			}
			got := textContent.Text
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want substring %q", got, tt.want)
			}
		})
	}
}
