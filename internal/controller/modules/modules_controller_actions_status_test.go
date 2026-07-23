package modules_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"

	. "github.com/onsi/gomega"
)

func TestGetReadyConditionType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		gvkKind  string
		expected string
	}{
		{
			name:     "AIGateway module",
			gvkKind:  "AIGateway",
			expected: "AIGatewayReady",
		},
		{
			name:     "Monitoring module",
			gvkKind:  "Monitoring",
			expected: "MonitoringReady",
		},
		{
			name:     "single word kind",
			gvkKind:  "Feast",
			expected: "FeastReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			handler := modules.BaseHandler{
				Config: modules.ModuleConfig{
					Name: "test",
					GVK: schema.GroupVersionKind{
						Group:   "components.platform.opendatahub.io",
						Version: "v1alpha1",
						Kind:    tt.gvkKind,
					},
				},
			}

			g.Expect(handler.GetReadyConditionType()).Should(Equal(tt.expected))
		})
	}
}

func TestGetReadyConditionType_MatchesComponentPattern(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	handler := modules.BaseHandler{
		Config: modules.ModuleConfig{
			Name: "aigateway",
			GVK: schema.GroupVersionKind{
				Kind: "AIGateway",
			},
		},
	}

	g.Expect(handler.GetReadyConditionType()).Should(Equal("AIGateway" + status.ReadySuffix))
}

func TestStatusMockHandler_ReadyConditionType(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	mock := newStatusMock("testmod", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
	})
	mock.Config.GVK = schema.GroupVersionKind{Kind: "TestModule"}

	g.Expect(mock.GetReadyConditionType()).Should(Equal("TestModuleReady"))
}
