package resources_test

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	res "github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

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

			got := resources.AnnotationChanged(annotationName).Update(event.UpdateEvent{
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

func TestDeploymentPredicateUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		old  *appsv1.Deployment
		new  *appsv1.Deployment
		want bool
	}{
		{
			name: "generation changed",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
			},
			want: true,
		},
		{
			name: "replicas changed",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 1},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2},
			},
			want: true,
		},
		{
			name: "ready replicas changed",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 1},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			},
			want: true,
		},
		{
			name: "no changes",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			},
			want: false,
		},
		{
			name: "empty objects",
			old:  &appsv1.Deployment{},
			new:  &appsv1.Deployment{},
			want: false,
		},
		{
			name: "no status in old object",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			},
			want: true,
		},
		{
			name: "no status in new object",
			old: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			},
			new: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			want: true,
		},
	}

	unstructuredDeployment := func(t *testing.T, g *WithT, deployment *appsv1.Deployment) *unstructured.Unstructured {
		t.Helper()

		u, err := res.ToUnstructured(deployment)
		g.Expect(err).ToNot(HaveOccurred())
		u.SetGroupVersionKind(gvk.Deployment)
		return u
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("structured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			predicate := resources.NewDeploymentPredicate()
			got := predicate.Update(event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run(fmt.Sprintf("unstructured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			predicate := resources.NewDeploymentPredicate()
			got := predicate.Update(event.UpdateEvent{
				ObjectOld: unstructuredDeployment(t, g, tt.old),
				ObjectNew: unstructuredDeployment(t, g, tt.new),
			})

			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestDeploymentPredicateUpdate_NilObjects(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	predicate := resources.NewDeploymentPredicate()

	// nil ObjectOld
	got := predicate.Update(event.UpdateEvent{
		ObjectOld: nil,
		ObjectNew: &appsv1.Deployment{},
	})
	g.Expect(got).To(BeFalse())

	// nil ObjectNew
	got = predicate.Update(event.UpdateEvent{
		ObjectOld: &appsv1.Deployment{},
		ObjectNew: nil,
	})
	g.Expect(got).To(BeFalse())

	// both nil
	got = predicate.Update(event.UpdateEvent{
		ObjectOld: nil,
		ObjectNew: nil,
	})
	g.Expect(got).To(BeFalse())
}

func TestDeploymentPredicateUpdate_WrongType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	predicate := resources.NewDeploymentPredicate()

	// old object is not a Deployment
	got := predicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &appsv1.Deployment{},
	})
	g.Expect(got).To(BeFalse())

	// new object is not a Deployment
	got = predicate.Update(event.UpdateEvent{
		ObjectOld: &appsv1.Deployment{},
		ObjectNew: &corev1.Pod{},
	})
	g.Expect(got).To(BeFalse())
}

func TestCMContentChangedPredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		oldData *corev1.ConfigMap
		newData *corev1.ConfigMap
		want    bool
	}{
		{
			name:    "data changed",
			oldData: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1"}, Data: map[string]string{"key": "old-value"}},
			newData: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2"}, Data: map[string]string{"key": "new-value"}},
			want:    true,
		},
		{
			name:    "data unchanged",
			oldData: &corev1.ConfigMap{Data: map[string]string{"key": "same-value"}},
			newData: &corev1.ConfigMap{Data: map[string]string{"key": "same-value"}},
			want:    false,
		},
		{
			name:    "resource version unchanged",
			oldData: &corev1.ConfigMap{Data: map[string]string{"key": "same-value"}},
			newData: &corev1.ConfigMap{Data: map[string]string{"key": "same-value"}},
			want:    false,
		},
		{
			name:    "key added",
			oldData: &corev1.ConfigMap{Data: map[string]string{}},
			newData: &corev1.ConfigMap{Data: map[string]string{"key": "value"}},
			want:    true,
		},
		{
			name:    "key removed",
			oldData: &corev1.ConfigMap{Data: map[string]string{"key": "value"}},
			newData: &corev1.ConfigMap{Data: map[string]string{}},
			want:    true,
		},
		{
			name:    "nil data in both",
			oldData: &corev1.ConfigMap{},
			newData: &corev1.ConfigMap{},
			want:    false,
		},
	}

	newUnstructuredCM := func(t *testing.T, g *WithT, data *corev1.ConfigMap) client.Object {
		t.Helper()

		u, err := res.ToUnstructured(data)
		g.Expect(err).ToNot(HaveOccurred())
		u.SetGroupVersionKind(gvk.ConfigMap)
		return u
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("structured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.CMContentChangedPredicate.Update(event.UpdateEvent{
				ObjectOld: tt.oldData,
				ObjectNew: tt.newData,
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run(fmt.Sprintf("unstructured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.CMContentChangedPredicate.Update(event.UpdateEvent{
				ObjectOld: newUnstructuredCM(t, g, tt.oldData),
				ObjectNew: newUnstructuredCM(t, g, tt.newData),
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run("create", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(resources.CMContentChangedPredicate.Create(event.CreateEvent{})).To(BeTrue())
		})

		t.Run("delete", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(resources.CMContentChangedPredicate.Delete(event.DeleteEvent{})).To(BeTrue())
		})

		t.Run("generic", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			g.Expect(resources.CMContentChangedPredicate.Generic(event.GenericEvent{})).To(BeTrue())
		})
	}
}

func TestCMContentChangedPredicate_WrongType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	// old object is not a ConfigMap
	got := resources.CMContentChangedPredicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.ConfigMap{},
	})
	g.Expect(got).To(BeFalse())

	// new object is not a ConfigMap
	got = resources.CMContentChangedPredicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.ConfigMap{},
		ObjectNew: &corev1.Pod{},
	})
	g.Expect(got).To(BeFalse())

	// both nil
	got = resources.CMContentChangedPredicate.Update(event.UpdateEvent{
		ObjectOld: nil,
		ObjectNew: nil,
	})
	g.Expect(got).To(BeFalse())
}

func TestSecretContentChangedPredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		oldData *corev1.Secret
		newData *corev1.Secret
		want    bool
	}{
		{
			name:    "data changed",
			oldData: &corev1.Secret{Data: map[string][]byte{"key": []byte("old-value")}},
			newData: &corev1.Secret{Data: map[string][]byte{"key": []byte("new-value")}},
			want:    true,
		},
		{
			name:    "data unchanged",
			oldData: &corev1.Secret{Data: map[string][]byte{"key": []byte("same-value")}},
			newData: &corev1.Secret{Data: map[string][]byte{"key": []byte("same-value")}},
			want:    false,
		},
		{
			name:    "resource version unchanged",
			oldData: &corev1.Secret{Data: map[string][]byte{"key": []byte("same-value")}},
			newData: &corev1.Secret{Data: map[string][]byte{"key": []byte("same-value")}},
			want:    false,
		},
		{
			name:    "key added",
			oldData: &corev1.Secret{Data: map[string][]byte{}},
			newData: &corev1.Secret{Data: map[string][]byte{"key": []byte("value")}},
			want:    true,
		},
		{
			name:    "key removed",
			oldData: &corev1.Secret{Data: map[string][]byte{"key": []byte("value")}},
			newData: &corev1.Secret{Data: map[string][]byte{}},
			want:    true,
		},
		{
			name:    "nil data in both",
			oldData: &corev1.Secret{},
			newData: &corev1.Secret{},
			want:    false,
		},
	}

	newUnstructuredSecret := func(t *testing.T, g *WithT, data *corev1.Secret) client.Object {
		t.Helper()

		if data == nil {
			return nil
		}

		u, err := res.ToUnstructured(data)
		g.Expect(err).ToNot(HaveOccurred())
		u.SetGroupVersionKind(gvk.Secret)
		return u
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.SecretContentChangedPredicate.Update(event.UpdateEvent{
				ObjectOld: tt.oldData,
				ObjectNew: tt.newData,
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run(fmt.Sprintf("unstructured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.SecretContentChangedPredicate.Update(event.UpdateEvent{
				ObjectOld: newUnstructuredSecret(t, g, tt.oldData),
				ObjectNew: newUnstructuredSecret(t, g, tt.newData),
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run("create", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(resources.SecretContentChangedPredicate.Create(event.CreateEvent{})).To(BeTrue())
		})

		t.Run("delete", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(resources.SecretContentChangedPredicate.Delete(event.DeleteEvent{})).To(BeTrue())
		})

		t.Run("generic", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			g.Expect(resources.SecretContentChangedPredicate.Generic(event.GenericEvent{})).To(BeTrue())
		})
	}
}

func TestSecretContentChangedPredicate_WrongType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	// old object is not a Secret
	got := resources.SecretContentChangedPredicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Secret{},
	})
	g.Expect(got).To(BeFalse())

	// new object is not a Secret
	got = resources.SecretContentChangedPredicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Secret{},
		ObjectNew: &corev1.Pod{},
	})
	g.Expect(got).To(BeFalse())
}

func TestDeleted(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	predicate := resources.Deleted()

	g.Expect(predicate.Create(event.CreateEvent{
		Object: &corev1.Pod{},
	})).To(BeFalse())

	g.Expect(predicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{},
	})).To(BeFalse())

	g.Expect(predicate.Delete(event.DeleteEvent{
		Object: &corev1.Pod{},
	})).To(BeTrue())

	g.Expect(predicate.Generic(event.GenericEvent{
		Object: &corev1.Pod{},
	})).To(BeFalse())
}

func TestDSCDeletionPredicate(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(resources.DSCDeletionPredicate.Delete(event.DeleteEvent{})).To(BeTrue())

	g.Expect(resources.DSCDeletionPredicate.Create(event.CreateEvent{})).To(BeTrue())

	g.Expect(resources.DSCDeletionPredicate.Update(event.UpdateEvent{})).To(BeTrue())

	g.Expect(resources.DSCDeletionPredicate.Generic(event.GenericEvent{})).To(BeTrue())
}

func TestDSCComponentUpdatePredicate_Structured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		oldDSC *dscv2.DataScienceCluster
		newDSC *dscv2.DataScienceCluster
		want   bool
	}{
		{
			name: "components spec changed",
			oldDSC: &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						Dashboard: componentApi.DSCDashboard{
							ManagementSpec: common.ManagementSpec{ManagementState: "Managed"},
						},
					},
				},
			},
			newDSC: &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						Dashboard: componentApi.DSCDashboard{
							ManagementSpec: common.ManagementSpec{ManagementState: "Removed"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "conditions count changed",
			oldDSC: &dscv2.DataScienceCluster{
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			newDSC: &dscv2.DataScienceCluster{
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
							{Type: "Available", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "condition status changed",
			oldDSC: &dscv2.DataScienceCluster{
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionFalse},
						},
					},
				},
			},
			newDSC: &dscv2.DataScienceCluster{
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no changes",
			oldDSC: &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						Dashboard: componentApi.DSCDashboard{
							ManagementSpec: common.ManagementSpec{ManagementState: "Managed"},
						},
					},
				},
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			newDSC: &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						Dashboard: componentApi.DSCDashboard{
							ManagementSpec: common.ManagementSpec{ManagementState: "Managed"},
						},
					},
				},
				Status: dscv2.DataScienceClusterStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			want: false,
		},
	}

	unstructuredDSC := func(t *testing.T, g *WithT, dsc *dscv2.DataScienceCluster) *unstructured.Unstructured {
		t.Helper()

		u, err := res.ToUnstructured(dsc)
		g.Expect(err).ToNot(HaveOccurred())
		u.SetGroupVersionKind(gvk.DataScienceCluster)
		return u
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("structured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.DSCComponentUpdatePredicate.Update(event.UpdateEvent{
				ObjectOld: tt.oldDSC,
				ObjectNew: tt.newDSC,
			})

			g.Expect(got).To(Equal(tt.want))
		})

		t.Run(fmt.Sprintf("unstructured %s", tt.name), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.DSCComponentUpdatePredicate.Update(event.UpdateEvent{
				ObjectOld: unstructuredDSC(t, g, tt.oldDSC),
				ObjectNew: unstructuredDSC(t, g, tt.newDSC),
			})

			g.Expect(got).To(Equal(tt.want))
		})
	}

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(resources.DSCComponentUpdatePredicate.Create(event.CreateEvent{})).To(BeTrue())
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(resources.DSCComponentUpdatePredicate.Delete(event.DeleteEvent{})).To(BeTrue())
	})

	t.Run("generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(resources.DSCComponentUpdatePredicate.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func TestDSCComponentUpdatePredicate_WrongType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	// old object is not a DSC
	got := resources.DSCComponentUpdatePredicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &dscv2.DataScienceCluster{},
	})
	g.Expect(got).To(BeFalse())

	// new object is not a DSC
	got = resources.DSCComponentUpdatePredicate.Update(event.UpdateEvent{
		ObjectOld: &dscv2.DataScienceCluster{},
		ObjectNew: &corev1.Pod{},
	})
	g.Expect(got).To(BeFalse())
}

func unstructuredPod(t *testing.T, g *WithT, pod *corev1.Pod) *unstructured.Unstructured {
	t.Helper()

	u, err := res.ToUnstructured(pod)
	g.Expect(err).ToNot(HaveOccurred())
	u.SetGroupVersionKind(gvk.Pod)
	return u
}

type namePredicateTestConfig struct {
	matchingName    string
	nonMatchingName string
	testCreate      bool
	testUpdate      bool
	testDelete      bool
	testGeneric     bool
}

func runNamePredicateTests(t *testing.T, pred predicate.Predicate, cfg namePredicateTestConfig) {
	t.Helper()

	type eventTest struct {
		name      string
		run       func(*testing.T, *WithT, client.Object) bool
		testIt    bool
		eventType string
	}

	eventTests := []eventTest{
		{
			name: "Create",
			run: func(_ *testing.T, _ *WithT, obj client.Object) bool {
				return pred.Create(event.CreateEvent{Object: obj})
			},
			testIt:    cfg.testCreate,
			eventType: "Create",
		},
		{
			name: "Update",
			run: func(_ *testing.T, _ *WithT, obj client.Object) bool {
				return pred.Update(event.UpdateEvent{ObjectNew: obj})
			},
			testIt:    cfg.testUpdate,
			eventType: "Update",
		},
		{
			name: "Delete",
			run: func(_ *testing.T, _ *WithT, obj client.Object) bool {
				return pred.Delete(event.DeleteEvent{Object: obj})
			},
			testIt:    cfg.testDelete,
			eventType: "Delete",
		},
		{
			name: "Generic",
			run: func(_ *testing.T, _ *WithT, obj client.Object) bool {
				return pred.Generic(event.GenericEvent{Object: obj})
			},
			testIt:    cfg.testGeneric,
			eventType: "Generic",
		},
	}

	for _, et := range eventTests {
		if !et.testIt {
			continue
		}

		t.Run(et.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			// Test structured - matching
			g.Expect(et.run(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: cfg.matchingName}})).
				To(BeTrue(), "%s - matching name - structured", et.eventType)

			// Test structured - non-matching
			g.Expect(et.run(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: cfg.nonMatchingName}})).
				To(BeFalse(), "%s - non-matching name - structured", et.eventType)

			// Test unstructured - matching
			g.Expect(et.run(t, g, unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: cfg.matchingName}}))).
				To(BeTrue(), "%s - matching name - unstructured", et.eventType)

			// Test unstructured - non-matching
			g.Expect(et.run(t, g, unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: cfg.nonMatchingName}}))).
				To(BeFalse(), "%s - non-matching name - unstructured", et.eventType)
		})
	}
}

func TestCreatedOrUpdatedName(t *testing.T) {
	t.Parallel()

	predicate := resources.CreatedOrUpdatedName("test-name")

	runNamePredicateTests(t, predicate, namePredicateTestConfig{
		matchingName:    "test-name",
		nonMatchingName: "other-name",
		testCreate:      true,
		testUpdate:      true,
		testDelete:      false, // Delete always returns true
		testGeneric:     false, // Generic always returns true
	})

	// FIXME: is correct to always return true with Delete predicate?
	t.Run("Delete", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Delete(event.DeleteEvent{})).To(BeTrue())
	})

	// FIXME: is correct to always return true with Generic predicate?
	t.Run("Generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func TestCreatedOrUpdatedOrDeletedNamed(t *testing.T) {
	t.Parallel()

	pred := resources.CreatedOrUpdatedOrDeletedNamed("test-name")

	runNamePredicateTests(t, pred, namePredicateTestConfig{
		matchingName:    "test-name",
		nonMatchingName: "other-name",
		testCreate:      true,
		testUpdate:      true,
		testDelete:      true,
		testGeneric:     false, // Generic always returns true
	})

	// FIXME: is correct to always return true with Generic predicate?
	t.Run("Generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func TestCreatedOrUpdatedOrDeletedNamePrefixed(t *testing.T) {
	t.Parallel()

	pred := resources.CreatedOrUpdatedOrDeletedNamePrefixed("test-")

	runNamePredicateTests(t, pred, namePredicateTestConfig{
		matchingName:    "test-pod",
		nonMatchingName: "other-pod",
		testCreate:      true,
		testUpdate:      true,
		testDelete:      true,
		testGeneric:     false, // Generic always returns true
	})

	// FIXME: is correct to always return true with Generic predicate?
	t.Run("Generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(pred.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func TestGatewayCertificateSecret(t *testing.T) {
	t.Parallel()

	isGatewayCert := func(obj client.Object) bool {
		return obj.GetName() == "gateway-cert"
	}

	predicate := resources.GatewayCertificateSecret(isGatewayCert)

	t.Run("Create", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Create(event.CreateEvent{
			Object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gateway-cert"}},
		})).To(BeTrue())

		g.Expect(predicate.Create(event.CreateEvent{
			Object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other-secret"}},
		})).To(BeFalse())
	})

	t.Run("Update", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Update(event.UpdateEvent{
			ObjectNew: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gateway-cert"}},
		})).To(BeTrue())

		g.Expect(predicate.Update(event.UpdateEvent{
			ObjectNew: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other-secret"}},
		})).To(BeFalse())
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Delete(event.DeleteEvent{
			Object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "gateway-cert"}},
		})).To(BeTrue())

		g.Expect(predicate.Delete(event.DeleteEvent{
			Object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other-secret"}},
		})).To(BeFalse())
	})

	// FIXME: is correct to always return true with Generic predicate?
	t.Run("Generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func TestGatewayStatusChanged(t *testing.T) {
	t.Parallel()

	predicate := resources.GatewayStatusChanged()

	t.Run("Create", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		// Create should return false
		g.Expect(predicate.Create(event.CreateEvent{
			Object: &corev1.Pod{},
		})).To(BeFalse())
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Delete(event.DeleteEvent{
			Object: &corev1.Pod{},
		})).To(BeTrue())
	})

	t.Run("Update", func(t *testing.T) {
		t.Parallel()

		t.Run("structured", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}},
				ObjectNew: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2", Generation: 1}},
			})).To(BeTrue())

			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}},
				ObjectNew: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2", Generation: 2}},
			})).To(BeFalse())

			// Update - no change
			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}},
				ObjectNew: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}},
			})).To(BeFalse())
		})

		t.Run("unstructured", func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}}),
				ObjectNew: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2", Generation: 1}}),
			})).To(BeTrue())

			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}}),
				ObjectNew: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2", Generation: 2}}),
			})).To(BeFalse())

			g.Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}}),
				ObjectNew: unstructuredPod(t, g, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}}),
			})).To(BeFalse())
		})
	})

	// FIXME: is correct to always return true with Generic predicate?
	t.Run("Generic", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)

		g.Expect(predicate.Generic(event.GenericEvent{})).To(BeTrue())
	})
}

func unstructuredHTTPRoute(t *testing.T, g *WithT, route *gwapiv1.HTTPRoute) *unstructured.Unstructured {
	t.Helper()

	u, err := res.ToUnstructured(route)
	g.Expect(err).ToNot(HaveOccurred())
	u.SetGroupVersionKind(gvk.HTTPRoute)
	return u
}

func TestHTTPRouteReferencesGateway(t *testing.T) {
	t.Parallel()

	predicate := resources.HTTPRouteReferencesGateway("my-gateway", "my-namespace")

	namespace := gwapiv1.Namespace("my-namespace")
	otherNamespace := gwapiv1.Namespace("other-namespace")

	matchingRoute := &gwapiv1.HTTPRoute{
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{Name: "my-gateway", Namespace: &namespace},
				},
			},
		},
	}

	nonMatchingRoute := &gwapiv1.HTTPRoute{
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{Name: "other-gateway", Namespace: &namespace},
				},
			},
		},
	}

	differentNamespaceRoute := &gwapiv1.HTTPRoute{
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{Name: "my-gateway", Namespace: &otherNamespace},
				},
			},
		},
	}

	resourceTypes := []struct {
		name      string
		transform func(t *testing.T, g *WithT, route *gwapiv1.HTTPRoute) client.Object
	}{
		{"structured", func(t *testing.T, _ *WithT, route *gwapiv1.HTTPRoute) client.Object {
			t.Helper()
			return route
		}},
		{"unstructured", func(t *testing.T, g *WithT, route *gwapiv1.HTTPRoute) client.Object {
			t.Helper()
			return unstructuredHTTPRoute(t, g, route)
		}},
	}

	for _, rt := range resourceTypes {
		t.Run(rt.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			eventTypes := []struct {
				name string
				run  func(client.Object) bool
			}{
				{"Create", func(obj client.Object) bool { return predicate.Create(event.CreateEvent{Object: obj}) }},
				{"Update", func(obj client.Object) bool { return predicate.Update(event.UpdateEvent{ObjectNew: obj}) }},
				{"Delete", func(obj client.Object) bool { return predicate.Delete(event.DeleteEvent{Object: obj}) }},
				{"Generic", func(obj client.Object) bool { return predicate.Generic(event.GenericEvent{Object: obj}) }},
			}

			tests := []struct {
				name     string
				object   client.Object
				expected bool
			}{
				{"matching", rt.transform(t, g, matchingRoute), true},
				{"non-matching gateway name", rt.transform(t, g, nonMatchingRoute), false},
				{"different namespace", rt.transform(t, g, differentNamespaceRoute), false},
				{"wrong type", &corev1.Pod{}, false},
			}

			for _, et := range eventTypes {
				t.Run(et.name, func(t *testing.T) {
					t.Parallel()
					for _, tt := range tests {
						t.Run(tt.name, func(t *testing.T) {
							g := NewWithT(t)

							g.Expect(et.run(tt.object)).To(Equal(tt.expected), "%s - %s", et.name, tt.name)
						})
					}
				})
			}
		})
	}
}

func TestHTTPRouteReferencesGateway_DefaultNamespace(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	predicate := resources.HTTPRouteReferencesGateway("my-gateway", "my-namespace")

	// HTTPRoute without explicit namespace (should default to gateway namespace)
	routeWithoutNamespace := &gwapiv1.HTTPRoute{
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{Name: "my-gateway"},
				},
			},
		},
	}

	g.Expect(predicate.Create(event.CreateEvent{Object: routeWithoutNamespace})).To(BeTrue())
}
