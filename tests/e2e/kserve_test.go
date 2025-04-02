package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func kserveTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	err = componentCtx.setUpServerless(t)
	require.NoError(t, err)

	err = componentCtx.createDummyFeatureTrackers(t)
	require.NoError(t, err)

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate component spec", componentCtx.validateSpec)
	t.Run("Validate component conditions", componentCtx.validateConditions)
	t.Run("Validate model controller", componentCtx.validateModelControllerInstance)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate no FeatureTracker OwnerReferences", componentCtx.ValidateNoFeatureTrackerOwnerReferences)
	t.Run("Validate no Kserve FeatureTrackers", componentCtx.ValidateNoKserveFeatureTrackers)
	t.Run("Validate default certs", componentCtx.validateDefaultCertsAvailable)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate serving transition to Unmanaged", componentCtx.ValidateServingTransitionToUnmanaged)
	t.Run("Validate serving transition to Removed", componentCtx.ValidateServingTransitionToRemoved)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

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

//nolint:thelper
func (c *KserveTestCtx) setUpServerless(t *testing.T) error {
	// TODO: removed once we know what's left on the cluster that's causing the tests
	//       to fail because of "existing KNativeServing resource was found"
	ksl := unstructured.UnstructuredList{}
	ksl.SetGroupVersionKind(gvk.KnativeServing)

	if err := c.Client().List(c.Context(), &ksl, client.InNamespace(knativeServingNamespace)); err != nil {
		return fmt.Errorf("error listing Knative Serving objects: %w", err)
	}

	if len(ksl.Items) != 0 {
		t.Logf("Detected %d Knative Serving objects in namespace %s", len(ksl.Items), knativeServingNamespace)
	}

	for _, obj := range ksl.Items {
		data, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("error marshalling Knative Serving object: %w", err)
		}

		t.Logf("Deleting Knative Serving %s in namespace %s: %s", obj.GetName(), obj.GetNamespace(), string(data))

		if err := c.Client().Delete(c.Context(), &obj); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("error deleting Knative Serving object: %w", err)
		}
	}

	return nil
}

func (c *KserveTestCtx) createDummyFeatureTrackers(_ *testing.T) error {
	ftNames := []string{
		c.ApplicationNamespace + "-serverless-serving-deployment",
		c.ApplicationNamespace + "-serverless-net-istio-secret-filtering",
		c.ApplicationNamespace + "-serverless-serving-gateways",
		c.ApplicationNamespace + "-kserve-external-authz",
	}

	for _, name := range ftNames {
		ft := &featuresv1.FeatureTracker{}
		ft.SetName(name)

		if _, err := controllerutil.CreateOrUpdate(c.Context(), c.Client(), ft, func() error {
			dsc, err := c.GetDSC()
			if err != nil {
				return err
			}
			if err := controllerutil.SetOwnerReference(dsc, ft, c.Client().Scheme()); err != nil {
				return err
			}
			ft.Spec.Source.Name = xid.New().String()

			return nil
		}); err != nil {
			return errors.New("error creating pre-existing FeatureTracker")
		}
	}

	return nil
}

func (c *KserveTestCtx) validateSpec(t *testing.T) {
	g := c.NewWithT(t)

	dsc, err := c.GetDSC()
	g.Expect(err).NotTo(HaveOccurred())

	g.List(gvk.Kserve).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.defaultDeploymentMode == "%s"`, dsc.Spec.Components.Kserve.DefaultDeploymentMode),
			jq.Match(`.spec.nim.managementState == "%s"`, dsc.Spec.Components.Kserve.NIM.ManagementState),
			jq.Match(`.spec.serving.managementState == "%s"`, dsc.Spec.Components.Kserve.Serving.ManagementState),
			jq.Match(`.spec.serving.name == "%s"`, dsc.Spec.Components.Kserve.Serving.Name),
			jq.Match(`.spec.serving.ingressGateway.certificate.type == "%s"`, dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.Type),
		)),
	))
}

func (c *KserveTestCtx) validateConditions(t *testing.T) {
	g := c.NewWithT(t)

	g.List(gvk.Kserve).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionTrue),
		)),
	))
}
func (c *KserveTestCtx) validateModelControllerInstance(t *testing.T) {
	g := c.NewWithT(t)

	g.List(gvk.ModelController).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue),
		)),
	))
}

func (c *KserveTestCtx) validateDefaultCertsAvailable(t *testing.T) {
	g := c.NewWithT(t)

	defaultIngressSecret, err := cluster.FindDefaultIngressSecret(g.Context(), g.Client())
	g.Expect(err).ToNot(HaveOccurred())

	dsc, err := c.GetDSC()
	g.Expect(err).ToNot(HaveOccurred())

	dsci, err := c.GetDSCI()
	g.Expect(err).ToNot(HaveOccurred())

	defaultSecretName := dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.SecretName
	if defaultSecretName == "" {
		defaultSecretName = "knative-serving-cert"
	}

	ctrlPlaneSecret, err := cluster.GetSecret(g.Context(), g.Client(), dsci.Spec.ServiceMesh.ControlPlane.Namespace, defaultSecretName)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(ctrlPlaneSecret.Type).Should(Equal(defaultIngressSecret.Type))
	g.Expect(defaultIngressSecret.Data).Should(Equal(ctrlPlaneSecret.Data))
}

func (c *KserveTestCtx) ValidateNoFeatureTrackerOwnerReferences(t *testing.T) {
	g := c.NewWithT(t)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(300).Should(And(
			jq.Match(`.metadata.ownerReferences | any(.kind == "%s")`, gvk.Kserve.Kind),
			jq.Match(`.metadata.ownerReferences | all(.kind != "%s")`, gvk.FeatureTracker.Kind),
		),
			`Checking if %s/%s in %s has expected owner refs`, child.gvk, child.nn.Name, child.nn.Namespace)
	}
}

func (c *KserveTestCtx) ValidateNoKserveFeatureTrackers(t *testing.T) {
	g := c.NewWithT(t)

	g.List(
		gvk.FeatureTracker,
	).Eventually(300).Should(
		HaveEach(And(
			jq.Match(`.metadata.name != "%s"`, c.ApplicationNamespace+"-kserve-external-authz"),
			jq.Match(`.metadata.name != "%s"`, c.ApplicationNamespace+"-serverless-serving-gateways"),
			jq.Match(`.metadata.name != "%s"`, c.ApplicationNamespace+"-serverless-serving-deployment"),
			jq.Match(`.metadata.name != "%s"`, c.ApplicationNamespace+"-serverless-net-istio-secret-filtering"),

			// there should be no FeatureTrackers owned by a Kserve
			jq.Match(`.metadata.ownerReferences | all(.kind != "%s")`, gvk.Kserve.Kind),
		)),
		`Ensuring there are no Kserve FeatureTrackers`)
}

func (c *KserveTestCtx) ValidateServingTransitionToUnmanaged(t *testing.T) {
	g := c.NewWithT(t)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(120).Should(And(
			jq.Match(`.metadata.labels | has("platform.opendatahub.io/part-of") == %v`, "true"),
			jq.Match(`.metadata.ownerReferences | length == %d`, 1),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, c.GVK.Kind),
		),
			`Ensuring %s/%s in %s has expected owner ref and part-of label`, child.gvk, child.nn.Name, child.nn.Namespace)
	}

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Unmanaged),
	).Eventually(120).Should(
		Succeed(),
		"Marking serving state as unmanaged",
	)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(120).Should(And(
			Not(BeNil()),
			jq.Match(`.metadata.labels | has("platform.opendatahub.io/part-of") == %v`, false),
			jq.Match(`.metadata.ownerReferences | length == %d`, 0),
		),
			`Ensuring %s/%s in %s still exists but is de-owned`, child.gvk, child.nn.Name, child.nn.Namespace)
	}

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	).Eventually(120).Should(
		Succeed(),
		"Resetting serving state to managed for subsequent tests",
	)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(120).Should(And(
			jq.Match(`.metadata.labels | has("platform.opendatahub.io/part-of") == %v`, "true"),
			jq.Match(`.metadata.ownerReferences | length == %d`, 1),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, c.GVK.Kind),
		),
			`Ensuring %s/%s in %s is re-owned`, child.gvk, child.nn.Name, child.nn.Namespace)
	}
}

func (c *KserveTestCtx) ValidateServingTransitionToRemoved(t *testing.T) {
	g := c.NewWithT(t)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(120).Should(And(
			jq.Match(`.metadata.labels | has("platform.opendatahub.io/part-of") == %v`, "true"),
			jq.Match(`.metadata.ownerReferences | length == %d`, 1),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, c.GVK.Kind),
		),
			`Ensuring %s/%s in %s has expected owner ref and part-of label`, child.gvk, child.nn.Name, child.nn.Namespace)
	}

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.defaultDeploymentMode = "%s"`, strings.ToLower(c.GVK.Kind), componentApi.RawDeployment),
	).Eventually(120).Should(
		Succeed(),
		"Setting defaultDeploymentMode to RawDeployment so that serving can be removed",
	)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Removed),
	).Eventually(120).Should(
		Succeed(),
		"Marking serving state as removed",
	)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(300).Should(And(
			BeNil(),
		),
			`Ensuring %s/%s in %s no longer exists`, child.gvk, child.nn.Name, child.nn.Namespace)
	}

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.serving.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	).Eventually(120).Should(
		Succeed(),
		"Marking serving state as managed for subsequent tests",
	)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.defaultDeploymentMode = "%s"`, strings.ToLower(c.GVK.Kind), componentApi.Serverless),
	).Eventually(120).Should(
		Succeed(),
		"Setting defaultDeploymentMode to Serverless for subsequent tests",
	)

	for _, child := range templatedResources {
		g.Get(child.gvk, child.nn).Eventually(120).Should(And(
			jq.Match(`.metadata.labels | has("platform.opendatahub.io/part-of") == %v`, "true"),
			jq.Match(`.metadata.ownerReferences | length == %d`, 1),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, c.GVK.Kind),
		),
			`Ensuring %s/%s in %s is re-created`, child.gvk, child.nn.Name, child.nn.Namespace)
	}
}
