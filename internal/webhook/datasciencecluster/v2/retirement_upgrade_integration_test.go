package v2_test

import (
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

const retirementUpgradeCRDName = "datascienceclusters.datasciencecluster.opendatahub.io"

// TestRetiredOperatorsAcrossSimulatedStorageUpgrade starts with a v2-only CRD,
// then installs the current v2/v3 CRD and verifies that the removed fields stay
// absent after a v3 storage rewrite.
// It is not a substitute for upgrading a released operator image on a cluster.
func TestRetiredOperatorsAcrossSimulatedStorageUpgrade(t *testing.T) {
	g := NewWithT(t)
	root, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())
	crdFile := filepath.Join(root, "config", "crd", "bases", "datasciencecluster.opendatahub.io_datascienceclusters.yaml")
	crdBytes, err := os.ReadFile(crdFile)
	g.Expect(err).NotTo(HaveOccurred())

	var currentCRD apiextensionsv1.CustomResourceDefinition
	g.Expect(yaml.Unmarshal(crdBytes, &currentCRD)).To(Succeed())
	oldCRD := currentCRD.DeepCopy()
	var oldV2 apiextensionsv1.CustomResourceDefinitionVersion
	for _, version := range oldCRD.Spec.Versions {
		if version.Name == dscv2.GroupVersion.Version {
			oldV2 = version
			break
		}
	}
	g.Expect(oldV2.Name).To(Equal(dscv2.GroupVersion.Version))
	oldV2.Storage = true
	oldV2.Served = true
	oldCRD.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{oldV2}
	oldCRD.Spec.Conversion = nil

	crdDir := t.TempDir()
	oldBytes, err := yaml.Marshal(oldCRD)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(os.WriteFile(filepath.Join(crdDir, filepath.Base(crdFile)), oldBytes, 0o600)).To(Succeed())

	env, err := envt.New(
		envt.WithCRDPaths(crdDir),
		envt.WithRegisterWebhooks(v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks),
	)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(env.Stop()).To(Succeed()) })
	env.StartManager(t, t.Context())
	g.Expect(env.WaitForWebhookServer(t.Context())).To(Succeed())

	ctx := t.Context()
	cli := env.Client()
	legacy := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "retired-operators-upgrade", Labels: map[string]string{"upgrade-sentinel": "retained"},
	}}
	legacy.Spec.Components.TrainingOperator.ManagementState = operatorv1.Removed
	legacy.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Removed
	legacy.Spec.Components.Workbenches.ManagementState = operatorv1.Removed
	g.Expect(cli.Create(ctx, legacy)).To(Succeed())

	legacy.Status.Components.TrainingOperator.ManagementState = operatorv1.Removed
	legacy.Status.Components.LlamaStackOperator.ManagementState = operatorv1.Removed
	legacy.Status.Conditions = []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "UpgradeSentinel", LastTransitionTime: metav1.Now()}}
	g.Expect(cli.Status().Update(ctx, legacy)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(legacy), legacy)).To(Succeed())
	g.Expect(legacy).To(WithTransform(resources.ToUnstructured, jq.Match(`
		.spec.components.trainingoperator.managementState == "Removed" and
		.spec.components.llamastackoperator.managementState == "Removed" and
		.status.components.trainingoperator.managementState == "Removed" and
		.status.components.llamastackoperator.managementState == "Removed"
	`)))
	legacy.Labels["unrelated-update"] = "allowed"
	g.Expect(cli.Update(ctx, legacy)).To(Succeed(), "an unrelated update must remain allowed")

	extensionsClient, err := apiextensionsclientset.NewForConfig(env.Config())
	g.Expect(err).NotTo(HaveOccurred())
	crdClient := extensionsClient.ApiextensionsV1().CustomResourceDefinitions()
	installedCRD, err := crdClient.Get(ctx, retirementUpgradeCRDName, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	installedCRD.Spec = currentCRD.Spec
	_, err = crdClient.Update(ctx, installedCRD, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(env.ConfigureCRDConversion(ctx, retirementUpgradeCRDName)).To(Succeed())

	key := client.ObjectKeyFromObject(legacy)
	v3 := &dscv3.DataScienceCluster{}
	g.Eventually(func() error { return cli.Get(ctx, key, v3) }).Should(Succeed())
	g.Expect(v3.Labels).To(HaveKeyWithValue("upgrade-sentinel", "retained"))
	g.Expect(v3.Spec.Components.Workbenches.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(v3.Status.Conditions).To(HaveLen(1))
	g.Expect(v3.Status.Conditions[0].Reason).To(Equal("UpgradeSentinel"))
	rawV3, err := env.DynamicClient().Resource(dscv3.GroupVersion.WithResource("datascienceclusters")).Get(ctx, key.Name, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rawV3).To(jq.Match(`
		(.spec.components | has("trainingoperator") | not) and
		(.spec.components | has("llamastackoperator") | not) and
		(.status.components | has("trainingoperator") | not) and
		(.status.components | has("llamastackoperator") | not)
	`))

	// A v3 rewrite changes storage version. The removed fields cannot be
	// reintroduced from the old v2 object on subsequent reads or writes.
	v3.Annotations = map[string]string{"upgrade-sentinel": "rewritten"}
	g.Expect(cli.Update(ctx, v3)).To(Succeed())
	v2Read := &dscv2.DataScienceCluster{}
	g.Expect(cli.Get(ctx, key, v2Read)).To(Succeed())
	g.Expect(v2Read).To(WithTransform(resources.ToUnstructured, jq.Match(`
		.spec.components.trainingoperator.managementState == "Removed" and
		.spec.components.llamastackoperator.managementState == "Removed" and
		.status.components.trainingoperator.managementState == "Removed" and
		.status.components.llamastackoperator.managementState == "Removed"
	`)))
	g.Expect(v2Read.Status.Conditions).To(HaveLen(1))
	g.Expect(v2Read.Labels).To(HaveKeyWithValue("upgrade-sentinel", "retained"))

	v2Read.Labels["unrelated-update"] = "allowed"
	g.Expect(cli.Update(ctx, v2Read)).To(Succeed())
	g.Expect(cli.Get(ctx, key, v2Read)).To(Succeed())
	v2Read.Spec.Components.TrainingOperator.ManagementState = operatorv1.Managed
	err = cli.Update(ctx, v2Read)
	g.Expect(err).To(MatchError(ContainSubstring("TrainingOperator v1 is obsolete")))
	g.Expect(cli.Get(ctx, key, v2Read)).To(Succeed())
	v2Read.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Managed
	err = cli.Update(ctx, v2Read)
	g.Expect(err).To(MatchError(ContainSubstring("LlamaStackOperator has been replaced by OGX")))
}
