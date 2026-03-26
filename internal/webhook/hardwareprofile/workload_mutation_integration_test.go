package hardwareprofile_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func createCPUWithTestKeyTolerationProfile(name, ns string) *infrav1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, ns,
		envtestutil.WithCPUIdentifier("1", "2"),
		envtestutil.WithNodeScheduling(
			map[string]string{"kubernetes.io/os": "linux"},
			[]corev1.Toleration{{
				Key: "test-key", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
			}},
		),
	)
}

func createCPUWithCustomTolerationProfile(name, ns, tolKey, osVal string) *infrav1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, ns,
		envtestutil.WithCPUIdentifier("1", "2"),
		envtestutil.WithNodeScheduling(
			map[string]string{"kubernetes.io/os": osVal},
			[]corev1.Toleration{{
				Key: tolKey, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
			}},
		),
	)
}

func getNotebookUnstructured(g Gomega, ctx context.Context, k8sClient client.Client, name, ns string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("kubeflow.org/v1")
	u.SetKind("Notebook")
	g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, u)).To(Succeed())
	return u
}

// TestHardwareProfileWebhook_Notebook_WorkloadMutations covers annotation removal, manual toleration merge,
// profile switching, and Kueue label cleanup (migrated from E2E hardwareprofile_test.go).
func TestHardwareProfileWebhook_Notebook_WorkloadMutations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "notebook - HWP annotation removal preserves manual scheduling",
			test: testNotebookHWPAnnotationRemovalPreservesManual,
		},
		{
			name: "notebook - manual tolerations merge when adding HWP",
			test: testNotebookManualTolerationMergeWithHWP,
		},
		{
			name: "notebook - profile switch replaces tolerations and nodeSelector",
			test: testNotebookProfileSwitchReplacesScheduling,
		},
		{
			name: "notebook - removing HWP clears kueue queue label",
			test: testNotebookKueueLabelRemovedWhenHWPIsRemoved,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithNotebook(),
			)
			defer teardown()

			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())
			g.Expect(infrav1.AddToScheme(env.Scheme())).To(Succeed())

			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

func testNotebookHWPAnnotationRemovalPreservesManual(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
	g.Expect(err).ShouldNot(HaveOccurred())

	hwp := createCPUWithTestKeyTolerationProfile("hwp-removal", ns)
	g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed())

	workloadName := "nb-hwp-removal"
	nb := envtestutil.NewNotebook(workloadName, ns,
		envtestutil.WithHardwareProfile(hwp.Name),
		envtestutil.WithHardwareProfileNamespace(ns),
	)
	g.Expect(k8sClient.Create(ctx, nb)).To(Succeed())

	u := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	tols, found, err := unstructured.NestedSlice(u.Object, config.TolerationsPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	tols = append(tols, map[string]interface{}{
		"key": "manual-key", "operator": "Exists", "effect": "NoSchedule",
	})
	g.Expect(unstructured.SetNestedSlice(u.Object, tols, config.TolerationsPath...)).To(Succeed())
	nsel, found, err := unstructured.NestedStringMap(u.Object, config.NodeSelectorPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	if !found || nsel == nil {
		nsel = map[string]string{}
	}
	nsel["manual-selector"] = "manual-value"
	g.Expect(unstructured.SetNestedStringMap(u.Object, nsel, config.NodeSelectorPath...)).To(Succeed())
	g.Expect(k8sClient.Update(ctx, u)).To(Succeed())

	u2 := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	ann := u2.GetAnnotations()
	delete(ann, hardwareprofilewebhook.HardwareProfileNameAnnotation)
	delete(ann, hardwareprofilewebhook.HardwareProfileNamespaceAnnotation)
	u2.SetAnnotations(ann)
	g.Expect(k8sClient.Update(ctx, u2)).To(Succeed())

	final := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	tolsOut, found, err := unstructured.NestedSlice(final.Object, config.TolerationsPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(tolsOut).Should(HaveLen(1))
	t0, ok := tolsOut[0].(map[string]interface{})
	g.Expect(ok).Should(BeTrue())
	g.Expect(t0["key"]).Should(Equal("manual-key"))

	nselOut, found, err := unstructured.NestedStringMap(final.Object, config.NodeSelectorPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(nselOut).Should(HaveKeyWithValue("manual-selector", "manual-value"))
	g.Expect(nselOut).ShouldNot(HaveKey("kubernetes.io/os"))
}

func testNotebookManualTolerationMergeWithHWP(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
	g.Expect(err).ShouldNot(HaveOccurred())

	hwp := createCPUWithTestKeyTolerationProfile("hwp-merge", ns)
	g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed())

	workloadName := "nb-manual-merge"
	nb := envtestutil.NewNotebook(workloadName, ns)
	g.Expect(k8sClient.Create(ctx, nb)).To(Succeed())

	u := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	g.Expect(unstructured.SetNestedSlice(u.Object, []interface{}{
		map[string]interface{}{"key": "manual-key", "operator": "Exists", "effect": "NoSchedule"},
	}, config.TolerationsPath...)).To(Succeed())
	g.Expect(unstructured.SetNestedStringMap(u.Object, map[string]string{"manual-selector": "manual-value"}, config.NodeSelectorPath...)).To(Succeed())
	g.Expect(k8sClient.Update(ctx, u)).To(Succeed())

	u2 := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	ann := u2.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[hardwareprofilewebhook.HardwareProfileNameAnnotation] = hwp.Name
	ann[hardwareprofilewebhook.HardwareProfileNamespaceAnnotation] = ns
	u2.SetAnnotations(ann)
	g.Expect(k8sClient.Update(ctx, u2)).To(Succeed())

	final := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	tolsOut, found, err := unstructured.NestedSlice(final.Object, config.TolerationsPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(tolsOut).Should(HaveLen(2))
	keys := make(map[string]bool)
	for _, ti := range tolsOut {
		m, ok := ti.(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		key, ok := m["key"].(string)
		g.Expect(ok).Should(BeTrue())
		keys[key] = true
	}
	g.Expect(keys).Should(HaveKey("manual-key"))
	g.Expect(keys).Should(HaveKey("test-key"))

	nselOut, found, err := unstructured.NestedStringMap(final.Object, config.NodeSelectorPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(nselOut).Should(HaveKeyWithValue("manual-selector", "manual-value"))
	g.Expect(nselOut).Should(HaveKeyWithValue("kubernetes.io/os", "linux"))
}

func testNotebookProfileSwitchReplacesScheduling(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
	g.Expect(err).ShouldNot(HaveOccurred())

	h1 := createCPUWithCustomTolerationProfile("hwp-first", ns, "first-key", "first-os")
	h2 := createCPUWithCustomTolerationProfile("hwp-second", ns, "second-key", "second-os")
	g.Expect(k8sClient.Create(ctx, h1)).To(Succeed())
	g.Expect(k8sClient.Create(ctx, h2)).To(Succeed())

	workloadName := "nb-profile-switch"
	nb := envtestutil.NewNotebook(workloadName, ns,
		envtestutil.WithHardwareProfile(h1.Name),
		envtestutil.WithHardwareProfileNamespace(ns),
	)
	g.Expect(k8sClient.Create(ctx, nb)).To(Succeed())

	u := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	ann := u.GetAnnotations()
	ann[hardwareprofilewebhook.HardwareProfileNameAnnotation] = h2.Name
	u.SetAnnotations(ann)
	g.Expect(k8sClient.Update(ctx, u)).To(Succeed())

	final := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	tolsOut, found, err := unstructured.NestedSlice(final.Object, config.TolerationsPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(tolsOut).Should(HaveLen(1))
	t0, ok := tolsOut[0].(map[string]interface{})
	g.Expect(ok).Should(BeTrue())
	g.Expect(t0["key"]).Should(Equal("second-key"))

	nselOut, found, err := unstructured.NestedStringMap(final.Object, config.NodeSelectorPath...)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(nselOut).Should(HaveKeyWithValue("kubernetes.io/os", "second-os"))
}

func testNotebookKueueLabelRemovedWhenHWPIsRemoved(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	hwp := createKueueHardwareProfile("hwp-kueue-removal", ns, "default")
	g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed())

	workloadName := "nb-kueue-removal"
	nb := envtestutil.NewNotebook(workloadName, ns,
		envtestutil.WithHardwareProfile(hwp.Name),
		envtestutil.WithHardwareProfileNamespace(ns),
	)
	g.Expect(k8sClient.Create(ctx, nb)).To(Succeed())

	g.Eventually(func() bool {
		u := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
		return resources.HasLabel(u, WorkloadLabelKueueQueueName, "default")
	}, "10s", "500ms").Should(BeTrue())

	u := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
	ann := u.GetAnnotations()
	delete(ann, hardwareprofilewebhook.HardwareProfileNameAnnotation)
	delete(ann, hardwareprofilewebhook.HardwareProfileNamespaceAnnotation)
	u.SetAnnotations(ann)
	g.Expect(k8sClient.Update(ctx, u)).To(Succeed())

	g.Eventually(func() bool {
		u2 := getNotebookUnstructured(g, ctx, k8sClient, workloadName, ns)
		return !resources.HasLabel(u2, WorkloadLabelKueueQueueName, "default")
	}, "10s", "500ms").Should(BeTrue())
}
