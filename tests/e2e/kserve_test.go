package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	gomegaTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	labelPartOfKey     = "platform.opendatahub.io/part-of"
	operatorDeployment = "opendatahub-operator-controller-manager"
)

type KserveTestCtx struct {
	*ComponentTestCtx
}

var templatedResources = []struct {
	gvk schema.GroupVersionKind
	nn  types.NamespacedName
}{
	{gvk.KnativeServing, types.NamespacedName{Namespace: "knative-serving", Name: "knative-serving"}},
	{gvk.ServiceMeshMember, types.NamespacedName{Namespace: "knative-serving", Name: "default"}},
	// {gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "activator-host-header"}},
	// {gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-after"}},
	// {gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-before"}},
	// {gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph-host-header"}},
	// {gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph"}},
	// {gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-predictor"}},
	{gvk.Gateway, types.NamespacedName{Namespace: "istio-system", Name: "kserve-local-gateway"}},
	{gvk.Gateway, types.NamespacedName{Namespace: "knative-serving", Name: "knative-ingress-gateway"}},
	{gvk.Gateway, types.NamespacedName{Namespace: "knative-serving", Name: "knative-local-gateway"}},
}

func kserveTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	// Increase the global eventually timeout
	reset := componentCtx.OverrideEventuallyTimeout(eventuallyTimeoutMedium, defaultEventuallyPollInterval)
	defer reset() // Make sure it's reset after all tests run

	// TODO: removed once we know what's left on the cluster that's causing the tests
	//       to fail because of "existing KNativeServing resource was found"
	t.Run("Setup Serverless", componentCtx.SetUpServerless)

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.validateComponentEnabled},
		{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate no FeatureTracker OwnerReferences", componentCtx.ValidateNoFeatureTrackerOwnerReferences},
		{"Validate no Kserve FeatureTrackers", componentCtx.ValidateNoKserveFeatureTrackers},
		{"Validate default certs", componentCtx.ValidateDefaultCertsAvailable},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate serving transition to Unmanaged", componentCtx.ValidateServingTransitionToUnmanaged},
		{"Validate serving transition to Removed", componentCtx.ValidateServingTransitionToRemoved},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	componentCtx.RunTestCases(t, testCases)
}

func (tc *KserveTestCtx) SetUpServerless(t *testing.T) {
	t.Helper()

	// TODO: removed once we know what's left on the cluster that's causing the tests
	//       to fail because of "existing KNativeServing resource was found"
	tc.cleanExistingKnativeServing(t)

	// Ensure the feature tracker resource is created or updated with the expected conditions.
	tc.createDummyFeatureTrackers()
}

// ValidateComponentEnabled ensures that the component is enabled and its status is "Ready".
func (tc *KserveTestCtx) validateComponentEnabled(t *testing.T) {
	t.Helper()

	// Define the test cases for checking component states
	testCases := []TestCase{
		{"Validate component enabled", tc.ValidateComponentEnabled},
		{"Validate serving enabled", tc.ValidateServingEnabled},
		{"Validate component spec", tc.ValidateSpec},
		{"Validate component conditions", tc.ValidateConditions},
	}

	// Run the test suite.
	tc.RunTestCases(t, testCases)
}

// ValidateSpec ensures that the Kserve instance configuration matches the expected specification.
func (tc *KserveTestCtx) ValidateServingEnabled(t *testing.T) {
	t.Helper()

	// Ensure the DataScienceCluster exists and the component's conditions are met
	tc.EnsureResourceCreatedOrUpdatedWithCondition(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		testf.TransformPipeline(
			testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed),
		),
		jq.Match(`.spec.components.%s.serving.managementState == "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed),
	)
}

// ValidateSpec ensures that the Kserve instance configuration matches the expected specification.
func (tc *KserveTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.RetrieveDataScienceCluster(tc.DataScienceClusterNamespacedName)

	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.Kserve,
		types.NamespacedName{Name: componentApi.KserveInstanceName},
		And(
			// Validate Kserve default deployment mode.
			jq.Match(`.spec.defaultDeploymentMode == "%s"`, dsc.Spec.Components.Kserve.DefaultDeploymentMode),
			// Validate management states of NIM and serving components.
			jq.Match(`.spec.nim.managementState == "%s"`, dsc.Spec.Components.Kserve.NIM.ManagementState),
			jq.Match(`.spec.serving.managementState == "%s"`, dsc.Spec.Components.Kserve.Serving.ManagementState),
			// Validate serving name and ingress certificate type.
			jq.Match(`.spec.serving.name == "%s"`, dsc.Spec.Components.Kserve.Serving.Name),
			jq.Match(`.spec.serving.ingressGateway.certificate.type == "%s"`, dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.Type),
		),
	)
}

// ValidateConditions validates that the Kserve instance's status conditions are correct.
func (tc *KserveTestCtx) ValidateConditions(t *testing.T) {
	t.Helper()

	// Ensure the Kserve resource has the "ServingAvailable" condition set to "True".
	tc.ValidateComponentCondition(
		gvk.Kserve,
		componentApi.KserveInstanceName,
		status.ConditionServingAvailable,
	)
}

// ValidateNoFeatureTrackerOwnerReferences ensures no FeatureTrackers are owned by Kserve.
func (tc *KserveTestCtx) ValidateNoFeatureTrackerOwnerReferences(t *testing.T) {
	t.Helper()

	for _, child := range templatedResources {
		tc.EnsureResourceExistsAndMatchesCondition(
			child.gvk,
			child.nn,
			And(
				jq.Match(`.metadata.ownerReferences | any(.kind == "%s")`, gvk.Kserve.Kind),
				jq.Match(`.metadata.ownerReferences | all(.kind != "%s")`, gvk.FeatureTracker.Kind),
			),
			`Checking if %s/%s in %s has expected owner refs`, child.gvk, child.nn.Name, child.nn.Namespace,
		)
	}
}

// ValidateNoKserveFeatureTrackers ensures there are no FeatureTrackers for Kserve.
func (tc *KserveTestCtx) ValidateNoKserveFeatureTrackers(t *testing.T) {
	t.Helper()

	tc.EnsureResourcesExistAndMatchCondition(
		gvk.FeatureTracker,
		tc.NamespacedName,
		nil,
		HaveEach(And(
			jq.Match(`.metadata.name != "%s"`, tc.AppsNamespace+"-kserve-external-authz"),
			jq.Match(`.metadata.name != "%s"`, tc.AppsNamespace+"-serverless-serving-gateways"),
			jq.Match(`.metadata.name != "%s"`, tc.AppsNamespace+"-serverless-serving-deployment"),
			jq.Match(`.metadata.name != "%s"`, tc.AppsNamespace+"-serverless-net-istio-secret-filtering"),

			// there should be no FeatureTrackers owned by a Kserve
			jq.Match(`.metadata.ownerReferences | all(.kind != "%s")`, gvk.Kserve.Kind),
		)),
		`Ensuring there are no Kserve FeatureTrackers`,
	)
}

// ValidateDefaultCertsAvailable ensures that the default ingress certificate matches the control plane secret in terms of Type and Data fields.
func (tc *KserveTestCtx) ValidateDefaultCertsAvailable(t *testing.T) {
	t.Helper()

	// Retrieve the default ingress secret used for ingress TLS termination.
	defaultIngressSecret, err := cluster.FindDefaultIngressSecret(tc.g.Context(), tc.g.Client())
	tc.g.Expect(err).ToNot(HaveOccurred())

	// Retrieve the DSCInitialization and DataScienceCluster instances.
	dsci := tc.RetrieveDSCInitialization(tc.DSCInitializationNamespacedName)
	dsc := tc.RetrieveDataScienceCluster(tc.DataScienceClusterNamespacedName)

	// Determine the control plane's ingress certificate secret name.
	defaultSecretName := dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.SecretName
	if defaultSecretName == "" {
		defaultSecretName = serverless.DefaultCertificateSecretName
	}

	// Fetch the control plane secret from the ServiceMesh namespace.
	ctrlPlaneSecret, err := cluster.GetSecret(tc.g.Context(), tc.g.Client(), dsci.Spec.ServiceMesh.ControlPlane.Namespace, defaultSecretName)
	tc.g.Expect(err).ToNot(HaveOccurred())

	// Validate that the secret types match.
	tc.EnsureResourcesAreEqual(
		ctrlPlaneSecret.Type, defaultIngressSecret.Type,
		"Secret type mismatch: Expected %v, but got %v", defaultIngressSecret.Type, ctrlPlaneSecret.Type,
	)

	// Validate that the secret data (certificate content) is identical.
	tc.EnsureResourcesAreEqual(
		ctrlPlaneSecret.Data, defaultIngressSecret.Data,
		"Secret data mismatch: Expected %v, but got %v", defaultIngressSecret.Data, ctrlPlaneSecret.Data,
	)
}

// ValidateServingTransitionToUnmanaged checks if serving transitions to unmanaged state.
func (tc *KserveTestCtx) ValidateServingTransitionToUnmanaged(t *testing.T) {
	t.Helper()

	tc.validateOwnerRefsAndLabels(true)

	tc.patchDscServingState(operatorv1.Unmanaged)
	tc.validateOwnerRefsAndLabels(false)

	tc.patchDscServingState(operatorv1.Managed)
	tc.validateOwnerRefsAndLabels(true)
}

// ValidateServingTransitionToRemoved checks if serving transitions to removed state.
func (tc *KserveTestCtx) ValidateServingTransitionToRemoved(t *testing.T) {
	t.Helper()

	tc.validateOwnerRefsAndLabels(true)

	tc.patchDscDeploymentMode(componentApi.RawDeployment)
	tc.patchDscServingState(operatorv1.Removed)

	for _, child := range templatedResources {
		tc.EnsureResourceGone(child.gvk, child.nn, `Ensuring %s/%s in %s no longer exists`, child.gvk, child.nn.Name, child.nn.Namespace)
	}

	tc.patchDscServingState(operatorv1.Managed)
	tc.patchDscDeploymentMode(componentApi.Serverless)
	tc.validateOwnerRefsAndLabels(true)
}

func (tc *KserveTestCtx) createDummyFeatureTrackers() {
	ftNames := []string{
		tc.AppsNamespace + "-serverless-serving-deployment",
		tc.AppsNamespace + "-serverless-net-istio-secret-filtering",
		tc.AppsNamespace + "-serverless-serving-gateways",
		tc.AppsNamespace + "-kserve-external-authz",
	}

	// Retrieve the DataScienceCluster instance.
	dsc := tc.RetrieveDataScienceCluster(tc.DataScienceClusterNamespacedName)

	for _, name := range ftNames {
		ft := &featuresv1.FeatureTracker{}
		ft.SetName(name)

		tc.EnsureResourceCreatedOrUpdated(
			WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: name}),
			func(obj *unstructured.Unstructured) error {
				if err := controllerutil.SetOwnerReference(dsc, obj, tc.Client().Scheme()); err != nil {
					return err
				}

				// trigger reconciliation as spec changes
				if err := unstructured.SetNestedField(obj.Object, xid.New().String(), "spec", "source", "name"); err != nil {
					return err
				}

				return nil
			},
			"error creating or updating pre-existing FeatureTracker",
		)
	}
}

func (tc *KserveTestCtx) cleanExistingKnativeServing(t *testing.T) {
	t.Helper()

	ksl := tc.RetrieveResources(gvk.KnativeServing, types.NamespacedName{Namespace: knativeServingNamespace}, &client.ListOptions{Namespace: knativeServingNamespace})

	if len(ksl) != 0 {
		t.Logf("Detected %d Knative Serving objects in namespace %s", len(ksl), knativeServingNamespace)
	}

	for _, obj := range ksl {
		data, err := json.Marshal(obj)
		tc.g.Expect(err).NotTo(HaveOccurred(), "error marshalling Knative Serving object: %w", err)

		t.Logf("Deleting Knative Serving %s in namespace %s: %s", obj.GetName(), obj.GetNamespace(), string(data))
		tc.DeleteResourceIfExists(gvk.KnativeServing, types.NamespacedName{Namespace: knativeServingNamespace, Name: obj.GetName()})

		// We also need to restart the Operator Pod.
		tc.DeleteResource(gvk.Deployment, types.NamespacedName{Namespace: tc.OperatorNamespace, Name: operatorDeployment})
	}
}

func (tc *KserveTestCtx) patchDscDeploymentMode(mode componentApi.DefaultDeploymentMode) {
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		testf.Transform(`.spec.components.%s.defaultDeploymentMode = "%s"`, strings.ToLower(tc.GVK.Kind), mode),
		"Updating defaultDeploymentMode",
	)
}

func (tc *KserveTestCtx) patchDscServingState(state operatorv1.ManagementState) {
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(tc.GVK.Kind), state),
		"Updating serving managementState",
	)
}

func (tc *KserveTestCtx) validateOwnerRefsAndLabels(expectOwned bool) {
	for _, child := range templatedResources {
		conds := []gomegaTypes.GomegaMatcher{}
		var msg string

		if expectOwned {
			conds = append(conds,
				jq.Match(`.metadata.labels | has("%s") == %v`, labelPartOfKey, true),
				jq.Match(`.metadata.ownerReferences | length == 1`),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
			)
			msg = `Ensuring %s/%s in %s has expected owner ref and part-of label`
		} else {
			conds = append(conds,
				jq.Match(`.metadata.labels | has("%s") == %v`, labelPartOfKey, false),
				jq.Match(`.metadata.ownerReferences | length == 0`),
			)
			msg = `Ensuring %s/%s in %s still exists but is de-owned`
		}
		tc.EnsureResourceExistsAndMatchesCondition(
			child.gvk,
			child.nn,
			And(conds...),
			msg, child.gvk, child.nn.Name, child.nn.Namespace)
	}
}
