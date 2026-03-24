package hardwareprofile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	infrav1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func hardwareProfileUnstructured(name, namespace, apiVersion string) *unstructured.Unstructured {
	minCount := intstr.FromInt32(1)
	maxCount := intstr.FromInt32(4)
	defaultCount := intstr.FromInt32(2)

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       "HardwareProfile",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"identifiers": []map[string]interface{}{
					{
						"displayName":  "GPU",
						"identifier":   "nvidia.com/gpu",
						"minCount":     minCount.IntVal,
						"maxCount":     maxCount.IntVal,
						"defaultCount": defaultCount.IntVal,
						"resourceType": "Accelerator",
					},
				},
				"scheduling": map[string]interface{}{
					"type": "Node",
					"node": map[string]interface{}{
						"nodeSelector": map[string]interface{}{
							"kubernetes.io/arch":             "amd64",
							"node-role.kubernetes.io/worker": "",
						},
						"tolerations": []map[string]interface{}{
							{
								"key":      "nvidia.com/gpu",
								"operator": "Exists",
								"effect":   "NoSchedule",
							},
						},
					},
				},
			},
		},
	}
}

// TestHardwareProfileAPIConversion exercises CRD conversion between v1alpha1 and v1 (migrated from E2E v2tov3upgrade_test.go).
func TestHardwareProfileAPIConversion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(projectDir, "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg).NotTo(BeNil())
	t.Cleanup(func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	})

	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(infrav1alpha1.AddToScheme(sch)).To(Succeed())

	k8sClient, err := client.New(cfg, client.Options{Scheme: sch})
	g.Expect(err).NotTo(HaveOccurred())

	ns := "hwp-conv-" + xid.New().String()
	g.Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	})).To(Succeed())

	t.Run("create v1alpha1 and read as v1", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		name := "test-hwp-v1alpha1-to-v1"
		u := hardwareProfileUnstructured(name, ns, infrav1alpha1.GroupVersion.String())
		g.Expect(k8sClient.Create(ctx, u)).To(Succeed())

		hv1 := &infrav1.HardwareProfile{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, hv1)).To(Succeed())
		g.Expect(hv1.Spec.Identifiers).ShouldNot(BeEmpty())
		g.Expect(hv1.Spec.Identifiers[0].Identifier).To(Equal("nvidia.com/gpu"))
		g.Expect(hv1.Spec.SchedulingSpec).NotTo(BeNil())
		g.Expect(hv1.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1.NodeScheduling))
		g.Expect(hv1.Spec.SchedulingSpec.Node).NotTo(BeNil())
		g.Expect(hv1.Spec.SchedulingSpec.Node.NodeSelector).To(HaveKeyWithValue("kubernetes.io/arch", "amd64"))

		g.Expect(k8sClient.Delete(ctx, hv1)).To(Succeed())
	})

	t.Run("create v1 and read as v1alpha1", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		name := "test-hwp-v1-to-v1alpha1"
		u := hardwareProfileUnstructured(name, ns, infrav1.GroupVersion.String())
		g.Expect(k8sClient.Create(ctx, u)).To(Succeed())

		ha := &infrav1alpha1.HardwareProfile{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ha)).To(Succeed())
		g.Expect(ha.Spec.Identifiers).ShouldNot(BeEmpty())
		g.Expect(ha.Spec.SchedulingSpec).NotTo(BeNil())
		g.Expect(ha.Spec.SchedulingSpec.SchedulingType).To(Equal(infrav1alpha1.NodeScheduling))

		g.Expect(k8sClient.Delete(ctx, ha)).To(Succeed())
	})
}
