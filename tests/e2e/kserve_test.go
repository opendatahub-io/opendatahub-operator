package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

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

	// TODO: removed once we know what's left on the cluster that's causing the tests
	//       to fail because of "existing KNativeServing resource was found"
	t.Run("setUpServerless", componentCtx.SetUpServerless)

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate component conditions", componentCtx.ValidateConditions},
		{"Validate FeatureTrackers", componentCtx.ValidateFeatureTrackers},
		{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate Kserve owns FeatureTrackers", componentCtx.ValidateKserveOwnsFeatureTrackers},
		{"Validate FeatureTrackers own children", componentCtx.ValidateFeatureTrackersOwnChildren},
		{"Validate KnativeServing Structure", componentCtx.ValidateKnativeServingStructure},
		{"Validate default certs", componentCtx.ValidateDefaultCertsAvailable},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
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
	ksl := tc.RetrieveResources(
		gvk.KnativeServing,
		types.NamespacedName{Namespace: knativeServingNamespace},
		&client.ListOptions{
			Namespace: knativeServingNamespace,
		},
	)

	if len(ksl) != 0 {
		t.Logf("Detected %d Knative Serving objects in namespace %s", len(ksl), knativeServingNamespace)
	}

	for _, obj := range ksl {
		data, err := json.Marshal(obj)
		tc.g.Expect(err).NotTo(HaveOccurred(), "error marshalling Knative Serving object: %w", err)

		t.Logf("Deleting Knative Serving %s in namespace %s: %s", obj.GetName(), obj.GetNamespace(), string(data))

		// Deleting the existing KnativeServing instance.
		tc.DeleteResourceIfExists(
			gvk.KnativeServing,
			types.NamespacedName{Namespace: knativeServingNamespace, Name: obj.GetName()},
		)

		// We also need to restart the Operator Pod.
		controllerDeployment := "opendatahub-operator-controller-manager"
		tc.DeleteResource(
			gvk.Deployment,
			types.NamespacedName{Namespace: tc.OperatorNamespace, Name: controllerDeployment},
		)
	}

	ftName := types.NamespacedName{Name: tc.AppsNamespace + "-serverless-serving-deployment"}

	// Retrieve the DataScienceCluster instance.
	dsc := tc.RetrieveDataScienceCluster(tc.DataScienceClusterNamespacedName)

	// Ensure the feature tracker resource is created or updated with the expected conditions.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.FeatureTracker, ftName),
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
		"error creating pre-existing FeatureTracker",
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

// ValidateFeatureTrackers ensures that the feature tracker configuration is correctly set.
func (tc *KserveTestCtx) ValidateFeatureTrackers(t *testing.T) {
	t.Helper()

	ftName := types.NamespacedName{Name: tc.AppsNamespace + "-serverless-serving-deployment"}

	// Retrieve the DataScienceCluster instance.
	dsc := tc.RetrieveDataScienceCluster(tc.DataScienceClusterNamespacedName)

	// Validate feature tracker specifications.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.FeatureTracker,
		ftName,
		And(
			jq.Match(`.metadata.ownerReferences | length == 1`),
			jq.Match(`.metadata.ownerReferences[0].apiVersion == "%s"`, gvk.Kserve.GroupVersion().String()),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Kserve.Kind),
			jq.Match(`.metadata.ownerReferences[0].blockOwnerDeletion == true`),
			jq.Match(`.metadata.ownerReferences[0].controller == true`),
		),
	)

	// Ensure the feature tracker resource is created or updated with the expected conditions.
	tc.EnsureResourceCreatedOrUpdatedWithCondition(
		WithMinimalObject(gvk.FeatureTracker, ftName),
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
		jq.Match(`(.metadata.ownerReferences | length) == 2`),
	)

	// Ensure the feature tracker has the correct ownership metadata.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.FeatureTracker,
		ftName,
		And(
			jq.Match(`.metadata.ownerReferences | length == 1`),
			jq.Match(`.metadata.ownerReferences[0].apiVersion == "%s"`, gvk.Kserve.GroupVersion().String()),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Kserve.Kind),
			jq.Match(`.metadata.ownerReferences[0].blockOwnerDeletion == true`),
			jq.Match(`.metadata.ownerReferences[0].controller == true`),
		),
	)
}

// ValidateModelControllerInstance ensures that the Model Controller instance is correctly set up.
func (tc *KserveTestCtx) ValidateModelControllerInstance(t *testing.T) {
	t.Helper()

	// Validate the existence of the ModelController instance.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.ModelController,
		types.NamespacedName{Name: componentApi.ModelControllerInstanceName},
		And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
		),
	)

	// Validate the DataScienceCluster's readiness.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.DataScienceCluster,
		tc.DataScienceClusterNamespacedName,
		And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue),
		),
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

// ValidateKserveOwnsFeatureTrackers ensures Kserve own the FeatureTracker resources.
func (tc *KserveTestCtx) ValidateKserveOwnsFeatureTrackers(t *testing.T) {
	t.Helper()

	// List of FeatureTracker names to validate.
	fts := []string{
		// c.ApplicationNamespace + "-kserve-external-authz",
		tc.AppsNamespace + "-serverless-serving-gateways",
		tc.AppsNamespace + "-serverless-serving-deployment",
		tc.AppsNamespace + "-serverless-net-istio-secret-filtering",
	}

	// Ensure Kserve owns each FeatureTracker.
	for _, ft := range fts {
		tc.EnsureResourceExistsAndMatchesCondition(
			gvk.FeatureTracker,
			types.NamespacedName{Name: ft},
			jq.Match(`.metadata.ownerReferences | any(.kind == "%s")`, gvk.Kserve.Kind),
			`Ensuring Kserve ownership of FeatureTracker %s`, ft,
		)
	}
}

// ValidateFeatureTrackersOwnChildren ensures FeatureTrackers own their child resources.
func (tc *KserveTestCtx) ValidateFeatureTrackersOwnChildren(t *testing.T) {
	t.Helper()

	// List of FeatureTracker children to validate.
	children := []struct {
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

	// Ensure each child resource is owned by the corresponding FeatureTracker.
	for _, child := range children {
		tc.EnsureResourceExistsAndMatchesCondition(
			child.gvk,
			child.nn,
			jq.Match(`.metadata.ownerReferences | any(.kind == "%s")`, gvk.FeatureTracker.Kind),
			`Checking if %s/%s in %s has expected owner refs`, child.gvk, child.nn.Name, child.nn.Namespace,
		)
	}
}

// ValidateKnativeServingStructure ensures that the KnativeServing object has the expected structure.
func (tc *KserveTestCtx) ValidateKnativeServingStructure(t *testing.T) {
	t.Helper()

	// Ensure KnativeServing has expected workloads and annotations.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.KnativeServing,
		types.NamespacedName{Namespace: "knative-serving", Name: "knative-serving"},
		And(
			jq.Match(`.spec.workloads | length == 3`),
			jq.Match(`.metadata.annotations."serverless.openshift.io/default-enable-http2" == "true"`),
		),
		`Ensuring KnativeServing has content from both templates`,
	)
}
