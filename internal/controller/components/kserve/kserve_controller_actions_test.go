//nolint:testpackage
package kserve

import (
	"testing"

	"github.com/onsi/gomega/gstruct"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_ServerlessUnmanaged(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		),
	)
}

func TestCheckPreConditions_ServiceMeshUnmanaged(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Removed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(
		MatchError(ContainSubstring(status.ServiceMeshNeedConfiguredMessage)),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoOperators(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(ContainSubstring(ErrServerlessOperatorNotInstalled.Error())),
		MatchError(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error()))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoServerlessOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(ContainSubstring(ErrServerlessOperatorNotInstalled.Error())),
		MatchError(Not(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error())))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoServiceMeshOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(Not(ContainSubstring(ErrServerlessOperatorNotInstalled.Error()))),
		MatchError(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error()))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_AllOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	// Set ServiceMesh condition to True since we're testing operator checks
	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}
	dsci.Status.Conditions = []common.Condition{
		{
			Type:   status.CapabilityServiceMesh,
			Status: metav1.ConditionTrue,
			Reason: "ServiceMeshReady",
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionTrue),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshConditionNotTrue(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	// Set ServiceMesh to Managed
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}
	// Set ServiceMesh condition to Unknown (not ready)
	dsci.Status.Conditions = []common.Condition{
		{
			Type:   status.CapabilityServiceMesh,
			Status: metav1.ConditionUnknown,
			Reason: "ServiceMeshNotReady",
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(
		MatchError(ContainSubstring(status.ServiceMeshNotReadyMessage)),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, status.ServiceMeshNotReadyMessage),
		)),
	)
}

func TestCheckPreConditions_RawServiceConfig(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.RawDeployment
	ksHeaded.Spec.RawDeploymentServiceConfig = componentApi.KserveRawHeaded

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Removed,
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ksHeaded).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		)),
	)

	ksHeadless := componentApi.Kserve{}
	ksHeadless.Spec.DefaultDeploymentMode = componentApi.RawDeployment
	ksHeadless.Spec.RawDeploymentServiceConfig = componentApi.KserveRawHeadless

	rrHeadless := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rrHeadless)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ksHeaded).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		)),
	)
}

func TestCleanUpTemplatedResources_withAuthorino(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
			&ofapiv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{
				Name: authorinoOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.Serverless
	ksHeaded.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = addTemplateFiles(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = template.NewAction(
		template.WithDataFn(getTemplateData),
	)(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cleanUpTemplatedResources(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rrHeaded.Resources).Should(
		And(
			HaveLen(11),
			ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.AuthorizationPolicy.Kind),
				),
			})),
			ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.EnvoyFilter.Kind),
				),
			})),
		),
	)
}

func TestCleanUpTemplatedResources_withoutAuthorino(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.Serverless
	ksHeaded.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = addTemplateFiles(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = template.NewAction(
		template.WithDataFn(getTemplateData),
	)(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cleanUpTemplatedResources(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rrHeaded.Resources).Should(
		And(
			HaveLen(7),
			Not(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.AuthorizationPolicy.Kind),
				),
			}))),
			Not(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.EnvoyFilter.Kind),
				),
			}))),
		),
	)
}

func TestCleanUpTemplatedResources_DoesNotDeleteExternalResources(t *testing.T) {
	// This test verifies that cleanUpTemplatedResources does NOT delete resources
	// that lack Kserve OwnerReferences, preventing accidental deletion of user-created
	// or externally-managed resources.
	//
	// Tests multiple code paths in cleanUpTemplatedResources:
	// 1. First deletion loop (ServiceMesh.ManagementState == Removed)
	// 2. Second deletion loop (!authorinoInstalled)

	testCases := []struct {
		name                   string
		authorinoInstalled     bool
		servingManagementState operatorv1.ManagementState
		serviceMeshManagement  operatorv1.ManagementState
		externalResourceGVK    schema.GroupVersionKind
		externalResourceName   string
		externalResourceNs     string
		description            string
	}{
		{
			name:                   "ServiceMesh Removed, Serving Unmanaged, with Authorino",
			authorinoInstalled:     true,
			servingManagementState: operatorv1.Unmanaged,
			serviceMeshManagement:  operatorv1.Removed,
			externalResourceGVK:    gvk.KnativeServing,
			externalResourceName:   "knative-serving",
			externalResourceNs:     "knative-serving",
			description:            "User wants to use existing KnativeServing but not manage it",
		},
		{
			name:                   "ServiceMesh Removed, Serving Removed, with Authorino",
			authorinoInstalled:     true,
			servingManagementState: operatorv1.Removed,
			serviceMeshManagement:  operatorv1.Removed,
			externalResourceGVK:    gvk.KnativeServing,
			externalResourceName:   "knative-serving",
			externalResourceNs:     "knative-serving",
			description:            "User wants RawDeployment mode without Serverless",
		},
		{
			name:                   "ServiceMesh Managed, No Authorino",
			authorinoInstalled:     false,
			servingManagementState: operatorv1.Managed,
			serviceMeshManagement:  operatorv1.Managed,
			externalResourceGVK:    gvk.EnvoyFilter,
			externalResourceName:   "activator-host-header",
			externalResourceNs:     "istio-system",
			description:            "Tests second deletion loop when authorino is not installed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			g := NewWithT(t)

			// Create an external resource WITHOUT Kserve OwnerReference.
			// This simulates a resource created by the user or another controller.
			externalResource := resources.GvkToUnstructured(tc.externalResourceGVK)
			externalResource.SetName(tc.externalResourceName)
			externalResource.SetNamespace(tc.externalResourceNs)
			// Intentionally not setting Kserve OwnerReference
			externalResource.SetLabels(map[string]string{
				"external-label": "true",
			})

			// Build initial fake client objects
			initialObjects := []client.Object{
				&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
					Name: serviceMeshOperator,
				}},
				&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
					Name: serverlessOperator,
				}},
				externalResource,
			}

			// Conditionally add authorino-operator subscription
			if tc.authorinoInstalled {
				initialObjects = append(initialObjects, &ofapiv1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "authorino-operator",
						Namespace: "openshift-operators",
					},
				})
			}

			cli, err := fakeclient.New(
				fakeclient.WithObjects(initialObjects...),
			)
			g.Expect(err).ShouldNot(HaveOccurred())

			ks := componentApi.Kserve{}
			ks.Spec.Serving.ManagementState = tc.servingManagementState
			ks.Spec.Serving.Name = "knative-serving"

			dsci := dsciv1.DSCInitialization{}
			dsci.Spec.ApplicationsNamespace = "opendatahub"
			dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
				ManagementState: tc.serviceMeshManagement,
				ControlPlane: infrav1.ControlPlaneSpec{
					Namespace: "istio-system",
				},
			}

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   &ks,
				DSCI:       &dsci,
				Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
			}

			// Add template files and render them
			err = addTemplateFiles(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			err = template.NewAction(
				template.WithDataFn(getTemplateData),
			)(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify the external resource exists before cleanup
			resourceBefore := resources.GvkToUnstructured(tc.externalResourceGVK)
			err = cli.Get(ctx, client.ObjectKey{Name: tc.externalResourceName, Namespace: tc.externalResourceNs}, resourceBefore)
			g.Expect(err).ShouldNot(HaveOccurred(), "External resource should exist before cleanup")

			// Execute the cleanup function
			err = cleanUpTemplatedResources(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// FIXED: The external resource should NOT be deleted because it doesn't have
			// a Kserve OwnerReference. The fix checks OwnerReferences before deletion in both:
			// 1. First deletion loop (ServiceMesh.ManagementState == Removed)
			// 2. Second deletion loop (!authorinoInstalled)
			resourceAfter := resources.GvkToUnstructured(tc.externalResourceGVK)
			err = cli.Get(ctx, client.ObjectKey{Name: tc.externalResourceName, Namespace: tc.externalResourceNs}, resourceAfter)
			g.Expect(err).ShouldNot(HaveOccurred(), "External resource should still exist (bug is fixed)")
		})
	}
}
