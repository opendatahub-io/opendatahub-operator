package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

func makeEvent(ns, name, kind, objName, etype, reason, msg string, lastTime time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: name, Namespace: ns},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: objName},
		Type:           etype,
		Reason:         reason,
		Message:        msg,
		LastTimestamp:  metav1.NewTime(lastTime),
	}
}

func TestRunRecentEvents(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name            string
		events          []client.Object
		cfg             clusterhealth.RecentEventsConfig
		skipClientSetup bool
		wantCount       int
		wantErr         bool
	}{
		{"default args", []client.Object{
			makeEvent("opendatahub", "evt1", "Pod", "pod-a", "Warning", "BackOff", "back-off", now.Add(-2*time.Minute)),
			makeEvent("opendatahub", "evt2", "Pod", "pod-b", "Normal", "Scheduled", "assigned", now.Add(-1*time.Minute)),
		}, clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub"}}, false, 2, false},

		{"since filter", []client.Object{
			makeEvent("opendatahub", "recent", "Pod", "pod-a", "Warning", "BackOff", "recent", now.Add(-3*time.Minute)),
			makeEvent("opendatahub", "old", "Pod", "pod-b", "Warning", "BackOff", "old", now.Add(-10*time.Minute)),
		}, clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub"}, Since: 5 * time.Minute}, false, 1, false},

		{"event type filter", []client.Object{
			makeEvent("opendatahub", "warn", "Pod", "pod-a", "Warning", "BackOff", "warning", now.Add(-1*time.Minute)),
			makeEvent("opendatahub", "norm", "Pod", "pod-b", "Normal", "Scheduled", "normal", now.Add(-1*time.Minute)),
		}, clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub"}, EventType: "Warning"}, false, 1, false},

		{"event type lowercase", []client.Object{
			makeEvent("opendatahub", "warn", "Pod", "pod-a", "Warning", "BackOff", "warning", now.Add(-1*time.Minute)),
			makeEvent("opendatahub", "norm", "Pod", "pod-b", "Normal", "Scheduled", "normal", now.Add(-1*time.Minute)),
		}, clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub"}, EventType: "warning"}, false, 1, false},

		{"empty result", nil,
			clusterhealth.RecentEventsConfig{Namespaces: []string{"empty-ns"}}, false, 0, false},

		{"empty namespaces", nil,
			clusterhealth.RecentEventsConfig{Namespaces: []string{}}, false, 0, false},

		{"nil client", nil,
			clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub"}}, true, 0, true},

		{"multi namespace no error", []client.Object{
			makeEvent("opendatahub", "evt1", "Pod", "pod-a", "Warning", "BackOff", "msg", now.Add(-1*time.Minute)),
		}, clusterhealth.RecentEventsConfig{Namespaces: []string{"opendatahub", "other-ns"}}, false, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.Client == nil && !tt.skipClientSetup {
				cl := newFakeClient(tt.events...)
				tt.cfg.Client = cl
			}
			events, err := clusterhealth.RunRecentEvents(context.Background(), tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(events) != tt.wantCount {
				t.Fatalf("got %d events, want %d", len(events), tt.wantCount)
			}
		})
	}
}

func TestRecentEvents_SortOrder(t *testing.T) {
	now := time.Now()
	cl := newFakeClient(
		makeEvent("opendatahub", "older", "Pod", "pod-a", "Warning", "BackOff", "older", now.Add(-3*time.Minute)),
		makeEvent("opendatahub", "newest", "Pod", "pod-b", "Warning", "BackOff", "newest", now.Add(-1*time.Minute)),
	)
	events, err := clusterhealth.RunRecentEvents(context.Background(), clusterhealth.RecentEventsConfig{
		Client: cl, Namespaces: []string{"opendatahub"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event, got none")
	}
	if events[0].Name != "pod-b" {
		t.Errorf("first event = %q, want pod-b (most recent)", events[0].Name)
	}
}

func TestRecentEvents_Count(t *testing.T) {
	now := time.Now()

	for _, count := range []int32{0, 1, 150} {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			evt := makeEvent("opendatahub", "evt1", "Pod", "pod-a", "Warning", "BackOff", "back-off", now.Add(-1*time.Minute))
			evt.Count = count
			cl := newFakeClient(evt)

			events, err := clusterhealth.RunRecentEvents(context.Background(), clusterhealth.RecentEventsConfig{
				Client: cl, Namespaces: []string{"opendatahub"},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1", len(events))
			}
			if events[0].Count != count {
				t.Errorf("Count = %d, want %d", events[0].Count, count)
			}
		})
	}
}

func TestRecentEvents_ErrorClients(t *testing.T) {
	tests := []struct {
		name      string
		client    client.Client
		args      map[string]any
		wantInErr string
	}{
		{"RBAC forbidden", newForbiddenClient(), map[string]any{"namespace": "opendatahub"}, "forbidden"},
		{"CRD not installed", newNoMatchClient(), map[string]any{"namespace": "opendatahub"}, "no matches for kind"},
		{"namespace discovery failed (RBAC)", newForbiddenClient(), map[string]any{}, "namespace discovery failed"},
		{"namespace discovery failed (CRD)", newNoMatchClient(), map[string]any{}, "namespace discovery failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := server.NewMCPServer("test", "0.0.1")
			registerRecentEvents(s, tt.client)

			msg, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{"name": "recent_events", "arguments": tt.args},
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			raw, err := json.Marshal(s.HandleMessage(context.Background(), msg))
			if err != nil {
				t.Fatalf("marshal handler response: %v", err)
			}

			var rpc struct {
				Result struct {
					Content []struct{ Text string } `json:"content"`
					IsError bool                    `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(raw, &rpc); err != nil {
				t.Fatalf("unmarshal rpc: %v", err)
			}
			if len(rpc.Result.Content) == 0 {
				t.Fatal("empty content")
			}
			if !strings.Contains(rpc.Result.Content[0].Text, tt.wantInErr) {
				t.Errorf("error text=%q, want substring %q", rpc.Result.Content[0].Text, tt.wantInErr)
			}
		})
	}
}

func TestDiscoverODHNamespaces(t *testing.T) {
	tests := []struct {
		name   string
		dsci   *unstructured.Unstructured
		wantNS string
	}{
		{"with DSCI", &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "dscinitialization.opendatahub.io/v2",
				"kind":       "DSCInitialization",
				"metadata":   map[string]interface{}{"name": "default-dsci"},
				"spec":       map[string]interface{}{"applicationsNamespace": "custom-apps"},
			},
		}, "custom-apps"},
		{"no DSCI", nil, defaultAppsNS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envApplicationsNamespace, "")
			t.Setenv(envOperatorNamespace, "")

			s := runtime.NewScheme()
			_ = scheme.AddToScheme(s)
			builder := fake.NewClientBuilder().WithScheme(s)
			if tt.dsci != nil {
				builder = builder.WithRuntimeObjects(tt.dsci)
			}
			cl := builder.Build()

			namespaces, err := discoverODHNamespaces(context.Background(), cl)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			found := false
			for _, ns := range namespaces {
				if ns == tt.wantNS {
					found = true
				}
			}
			if !found {
				t.Errorf("namespaces = %v, want %q", namespaces, tt.wantNS)
			}
		})
	}
}
