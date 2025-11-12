package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type KserveTestCtx struct {
	*ComponentTestCtx
}

func kserveTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	// Increase the global eventually timeout
	reset := componentCtx.OverrideEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout, ct.TestTimeouts.defaultEventuallyPollInterval)
	defer reset() // Make sure it's reset after all tests run

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate no Kserve FeatureTrackers", componentCtx.ValidateNoKserveFeatureTrackers},
		{"Validate VAP created when kserve is enabled", componentCtx.ValidateS3SecretCheckBucketExist},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
	}

	// Add webhook tests if enabled
	if testOpts.webhookTest {
		testCases = append(testCases,
			TestCase{"Validate connection webhook injection", componentCtx.ValidateConnectionWebhookInjection},
		)
	}

	// Always run deletion recovery and component disable tests last
	testCases = append(testCases,
		TestCase{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		TestCase{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	)
	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSpec ensures that the Kserve instance configuration matches the expected specification.
func (tc *KserveTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
		WithCondition(And(
			// Validate management states of NIM and serving components.
			jq.Match(`.spec.nim.managementState == "%s"`, dsc.Spec.Components.Kserve.NIM.ManagementState),
		),
		),
	)
}

// ValidateNoKserveFeatureTrackers ensures there are no FeatureTrackers for Kserve.
func (tc *KserveTestCtx) ValidateNoKserveFeatureTrackers(t *testing.T) {
	t.Helper()

	tc.EnsureResourcesDoNotExist(
		WithMinimalObject(gvk.FeatureTracker, tc.NamespacedName),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(
				k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				},
			),
		}),
		WithCustomErrorMsg("Expected no KServe-related FeatureTracker resources to be present"),
	)
}

// ValidateConnectionWebhookInjection validates that the connection webhook properly injects
// secrets into InferenceService resources with existing imagePullSecrets.
func (tc *KserveTestCtx) ValidateConnectionWebhookInjection(t *testing.T) {
	t.Helper()

	// Ensure KServe is in Managed state to enable webhook functionality
	tc.ValidateComponentEnabled(t)

	testNamespace := "glue-namespace"
	secretName := "glue-secret"
	isvcName := "glue-isvc"

	// Create test namespace
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithCustomErrorMsg("Failed to create webhook test namespace"),
	)

	// Create a connection secret with OCI type
	tc.createConnectionSecret(secretName, testNamespace)

	// Create InferenceService with connection annotation and existing imagePullSecrets
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.InferenceServices, types.NamespacedName{Name: isvcName, Namespace: testNamespace}),
		WithMutateFunc(testf.TransformPipeline(
			// Set connection annotation
			testf.Transform(`.metadata.annotations."%s" = "%s"`, annotations.Connection, secretName),
			// Set predictor spec with model and existing imagePullSecrets
			testf.Transform(`.spec.predictor = {
				"model": {},
				"imagePullSecrets": [{"name": "existing-secret"}]
			}`),
		)),
		WithCustomErrorMsg("Failed to create InferenceService with webhook injection"),
	)

	// Validate that both the existing-secret and the new connection secret are present
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.InferenceServices, types.NamespacedName{Name: isvcName, Namespace: testNamespace}),
		WithCondition(jq.Match(`
			.spec.predictor.imagePullSecrets | length == 2
			and (map(.name) | contains(["existing-secret"]))
			and (map(.name) | contains(["%s"]))`,
			secretName)),
		WithCustomErrorMsg("InferenceService should have both existing and injected imagePullSecrets"),
	)

	// Cleanup the created test namespace
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)
}

// createConnectionSecret creates a connection secret with OCI type to test webhook.
func (tc *KserveTestCtx) createConnectionSecret(secretName, namespace string) {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Secret, types.NamespacedName{Name: secretName, Namespace: namespace}),
		WithMutateFunc(testf.TransformPipeline(
			// Set connection type annotation
			testf.Transform(`.metadata.annotations."%s" = "%s"`, annotations.ConnectionTypeProtocol, "oci"),
			// Set secret type
			testf.Transform(`.type = "%s"`, string(corev1.SecretTypeOpaque)),
			// Set secret data
			testf.Transform(`.data = {"credential": "mysecretjson"}`),
		)),
		WithCustomErrorMsg("Failed to create connection secret"),
	)
}
