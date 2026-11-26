//nolint:testpackage // Exercises the unexported trackerReleaseMatches helper.
package modules

import (
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"

	. "github.com/onsi/gomega"
)

type statusMockHandler struct {
	BaseHandler

	status *ModuleStatus
}

func newStatusMock(name string, moduleStatus *ModuleStatus) *statusMockHandler {
	return &statusMockHandler{
		BaseHandler: BaseHandler{
			Config: ModuleConfig{Name: name, CRName: "default"},
		},
		status: moduleStatus,
	}
}

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

			handler := BaseHandler{
				Config: ModuleConfig{
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

	handler := BaseHandler{
		Config: ModuleConfig{
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

	mock := newStatusMock("testmod", &ModuleStatus{
		Conditions: []common.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
	})
	mock.Config.GVK = schema.GroupVersionKind{Kind: "TestModule"}

	g.Expect(mock.GetReadyConditionType()).Should(Equal("TestModuleReady"))
}

func TestTrackerReleaseMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  common.Release
		tracker common.Release
		want    bool
	}{
		{
			name:    "matching non-zero release",
			target:  common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			tracker: common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			want:    true,
		},
		{
			name:    "version mismatch",
			target:  common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			tracker: common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.6.0")},
			want:    false,
		},
		{
			name:    "name mismatch",
			target:  common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			tracker: common.Release{Name: "RHAI", Version: mustSemver(t, "3.7.0")},
			want:    false,
		},
		{
			name:    "unset tracker release is not a match for a real target",
			target:  common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			tracker: common.Release{Name: "Open Data Hub"},
			want:    false,
		},
		{
			name:    "zero target version accepts matching zero tracker (dev builds)",
			target:  common.Release{Name: "Open Data Hub", Version: ofversion.OperatorVersion{}},
			tracker: common.Release{Name: "Open Data Hub", Version: ofversion.OperatorVersion{}},
			want:    true,
		},
		{
			name:    "zero target version rejects non-zero tracker",
			target:  common.Release{Name: "Open Data Hub", Version: ofversion.OperatorVersion{}},
			tracker: common.Release{Name: "Open Data Hub", Version: mustSemver(t, "3.7.0")},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := trackerReleaseMatches(tt.target, tt.tracker); got != tt.want {
				t.Errorf("trackerReleaseMatches(%+v, %+v) = %v, want %v", tt.target, tt.tracker, got, tt.want)
			}
		})
	}
}

func mustSemver(t *testing.T, v string) ofversion.OperatorVersion {
	t.Helper()
	parsed, err := semver.Parse(v)
	if err != nil {
		t.Fatalf("parsing %q: %v", v, err)
	}
	return ofversion.OperatorVersion{Version: parsed}
}
