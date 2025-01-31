package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

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

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate component spec", componentCtx.validateSpec)
	t.Run("Validate FeatureTrackers", componentCtx.validateFeatureTrackers)
	t.Run("Validate model controller", componentCtx.validateModelControllerInstance)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate default certs", componentCtx.validateDefaultCertsAvailable)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	// t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
}

type KserveTestCtx struct {
	*ComponentTestCtx
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

	ft := &featuresv1.FeatureTracker{}
	ft.SetName(c.ApplicationNamespace + "-serverless-serving-deployment")

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

func (c *KserveTestCtx) validateFeatureTrackers(t *testing.T) {
	g := c.NewWithT(t)
	ftName := types.NamespacedName{Name: c.ApplicationNamespace + "-serverless-serving-deployment"}

	g.Get(gvk.FeatureTracker, ftName).Eventually().Should(And(
		jq.Match(`(.metadata.ownerReferences | length) == 1`),
		jq.Match(`.metadata.ownerReferences[0].apiVersion == "%s"`, gvk.Kserve.GroupVersion().String()),
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Kserve.Kind),
		jq.Match(`.metadata.ownerReferences[0].blockOwnerDeletion == true`),
		jq.Match(`.metadata.ownerReferences[0].controller == true`),
	))

	dsc, err := c.GetDSC()
	g.Expect(err).NotTo(HaveOccurred())

	g.Update(
		gvk.FeatureTracker,
		ftName,
		func(obj *unstructured.Unstructured) error {
			if err := controllerutil.SetOwnerReference(dsc, obj, c.Client().Scheme()); err != nil {
				return err
			}

			// trigger reconciliation as spec changes
			if err = unstructured.SetNestedField(obj.Object, xid.New().String(), "spec", "source", "name"); err != nil {
				return err
			}

			return nil
		},
	).Eventually().Should(And(
		jq.Match(`(.metadata.ownerReferences | length) == 2`),
	))

	g.Get(gvk.FeatureTracker, ftName).Eventually().Should(And(
		jq.Match(`(.metadata.ownerReferences | length) == 1`),
		jq.Match(`.metadata.ownerReferences[0].apiVersion == "%s"`, gvk.Kserve.GroupVersion().String()),
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Kserve.Kind),
		jq.Match(`.metadata.ownerReferences[0].blockOwnerDeletion == true`),
		jq.Match(`.metadata.ownerReferences[0].controller == true`),
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
		defaultSecretName = serverless.DefaultCertificateSecretName
	}

	ctrlPlaneSecret, err := cluster.GetSecret(g.Context(), g.Client(), dsci.Spec.ServiceMesh.ControlPlane.Namespace, defaultSecretName)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(ctrlPlaneSecret.Type).Should(Equal(defaultIngressSecret.Type))
	g.Expect(defaultIngressSecret.Data).Should(Equal(ctrlPlaneSecret.Data))
}
