package resources_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"

	. "github.com/onsi/gomega"
)

func TestAnnotationChanged(t *testing.T) {
	t.Parallel()

	const annotationName = "test-annotation"

	tests := []struct {
		name           string
		annotation     string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		want           bool
	}{
		{
			name:           "annotation value changed",
			oldAnnotations: map[string]string{annotationName: "old-value"},
			newAnnotations: map[string]string{annotationName: "new-value"},
			want:           true,
		},
		{
			name:           "annotation value unchanged",
			oldAnnotations: map[string]string{annotationName: "same-value"},
			newAnnotations: map[string]string{annotationName: "same-value"},
			want:           false,
		},
		{
			name:           "annotation added",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{annotationName: "new-value"},
			want:           true,
		},
		{
			name:           "annotation removed",
			oldAnnotations: map[string]string{annotationName: "old-value"},
			newAnnotations: map[string]string{},
			want:           true,
		},
		{
			name:           "different annotation changed",
			oldAnnotations: map[string]string{annotationName: "value", "other-annotation": "old-value"},
			newAnnotations: map[string]string{annotationName: "value", "other-annotation": "new-value"},
			want:           false,
		},
		{
			name:           "nil annotations in both objects",
			oldAnnotations: nil,
			newAnnotations: nil,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.AnnotationChanged(annotationName).UpdateFunc(event.UpdateEvent{
				ObjectOld: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-pod",
						Annotations: tt.oldAnnotations,
					},
				},
				ObjectNew: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-pod",
						Annotations: tt.newAnnotations,
					},
				},
			})

			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestDSCIServiceMeshCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		oldObj   *dsciv1.DSCInitialization
		newObj   *dsciv1.DSCInitialization
		expected bool
	}{
		{
			name: "when new condition is added (length changed)",
			oldObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{},
				},
			},
			newObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "when old condition is removed(length changed)",
			oldObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			newObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{},
				},
			},
			expected: true,
		},
		{
			name: "when condition status changes(SMCP to ready)",
			oldObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			newObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "when condition status remains the same",
			oldObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			newObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMesh,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "when other condition changes",
			oldObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMeshAuthorization,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			newObj: &dsciv1.DSCInitialization{
				Status: dsciv1.DSCInitializationStatus{
					Conditions: []common.Condition{
						{
							Type:   status.CapabilityServiceMeshAuthorization,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			e := event.UpdateEvent{
				ObjectOld: tt.oldObj,
				ObjectNew: tt.newObj,
			}
			g.Expect(resources.DSCIServiceMeshCondition.UpdateFunc(e)).To(Equal(tt.expected))
		})
	}

	// Test Create, Delete, and Generic events
	t.Run("Create event returns false", func(t *testing.T) {
		g := NewWithT(t)
		e := event.CreateEvent{}
		g.Expect(resources.DSCIServiceMeshCondition.CreateFunc(e)).To(BeFalse())
	})

	t.Run("Delete event returns false", func(t *testing.T) {
		g := NewWithT(t)
		e := event.DeleteEvent{}
		g.Expect(resources.DSCIServiceMeshCondition.DeleteFunc(e)).To(BeFalse())
	})

	t.Run("Generic event returns false", func(t *testing.T) {
		g := NewWithT(t)
		e := event.GenericEvent{}
		g.Expect(resources.DSCIServiceMeshCondition.GenericFunc(e)).To(BeFalse())
	})
}
