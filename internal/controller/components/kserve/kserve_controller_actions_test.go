//nolint:testpackage
package kserve

import (
	"testing"

	"github.com/onsi/gomega/gstruct"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestCleanUpTemplatedResources_DeletesResourcesWithoutKserveLabel(t *testing.T) {
	// This test demonstrates the bug where cleanUpTemplatedResources deletes KnativeServing
	// resources based on name/namespace alone, without checking if the cluster resource
	// has the platform.opendatahub.io/part-of: kserve ownership label.
	//
	// The code iterates through rr.Resources (which come from templates and have the
	// platform.opendatahub.io/dependency: serverless label), and for each matching resource,
	// it deletes any cluster resource with the same name/namespace regardless of whether
	// that cluster resource was actually created by the Kserve controller.
	//
	// Both scenarios from RHOAIENG-37741 trigger this bug.

	testCases := []struct {
		name                     string
		servingManagementState   operatorv1.ManagementState
		serviceMeshManagement    operatorv1.ManagementState
		description              string
	}{
		{
			name:                   "Scenario 1: ServiceMesh Removed, Serving Unmanaged",
			servingManagementState: operatorv1.Unmanaged,
			serviceMeshManagement:  operatorv1.Removed,
			description:            "User wants to use existing KnativeServing but not manage it",
		},
		{
			name:                   "Scenario 2: ServiceMesh Removed, Serving Removed",
			servingManagementState: operatorv1.Removed,
			serviceMeshManagement:  operatorv1.Removed,
			description:            "User wants RawDeployment mode without Serverless",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			g := NewWithT(t)

			// Create a KnativeServing resource WITHOUT the platform.opendatahub.io/part-of: kserve label.
			// This simulates a KnativeServing CR that was NOT created by the Kserve controller,
			// but was instead created by the user or another controller for a different purpose.
			externalKnativeServing := resources.GvkToUnstructured(gvk.KnativeServing)
			externalKnativeServing.SetName("knative-serving")
			externalKnativeServing.SetNamespace("knative-serving")
			// Intentionally not setting platform.opendatahub.io/part-of: kserve label
			externalKnativeServing.SetLabels(map[string]string{
				"external-label": "true",
			})

			cli, err := fakeclient.New(
				fakeclient.WithObjects(
					&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
						Name: serviceMeshOperator,
					}},
					&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
						Name: serverlessOperator,
					}},
					externalKnativeServing,
				),
			)
			g.Expect(err).ShouldNot(HaveOccurred())

			ks := componentApi.Kserve{}
			ks.Spec.Serving.ManagementState = tc.servingManagementState
			ks.Spec.Serving.Name = "knative-serving" // Explicitly set to match the resource name

			// ServiceMesh set to Removed triggers the deletion code path on line 288-306
			dsci := dsciv1.DSCInitialization{}
			dsci.Spec.ApplicationsNamespace = "opendatahub" // Required for template rendering
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

			// Add template files which will generate a KnativeServing resource in rr.Resources.
			// This template resource will have the platform.opendatahub.io/dependency: serverless label.
			err = addTemplateFiles(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			err = template.NewAction(
				template.WithDataFn(getTemplateData),
			)(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify the external KnativeServing exists before cleanup
			knativeServingBefore := resources.GvkToUnstructured(gvk.KnativeServing)
			err = cli.Get(ctx, client.ObjectKey{Name: "knative-serving", Namespace: "knative-serving"}, knativeServingBefore)
			g.Expect(err).ShouldNot(HaveOccurred(), "KnativeServing should exist before cleanup")

			// Execute the cleanup function
			err = cleanUpTemplatedResources(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// BUG: The external KnativeServing resource gets deleted even though it doesn't have
			// the platform.opendatahub.io/part-of: kserve label.
			//
			// This happens on line 294 of kserve_controller_actions.go where the code:
			// 1. Iterates through rr.Resources (which contains the template resource with dependency label)
			// 2. For each resource matching isForDependency("serverless"), it calls rr.Client.Delete(&res, ...)
			// 3. The delete uses the name/namespace from the template resource (&res)
			// 4. This deletes ANY cluster resource with that name/namespace, regardless of labels
			//
			// The fix should be to fetch the cluster resource first and check if it has the
			// platform.opendatahub.io/part-of: kserve label before deleting.
			knativeServingAfter := resources.GvkToUnstructured(gvk.KnativeServing)
			err = cli.Get(ctx, client.ObjectKey{Name: "knative-serving", Namespace: "knative-serving"}, knativeServingAfter)
			g.Expect(err).Should(HaveOccurred(), "KnativeServing should have been deleted (demonstrates the bug)")
			g.Expect(err.Error()).Should(ContainSubstring("not found"))
		})
	}
}

func TestCleanUpTemplatedResources_DeletesResourcesWithoutKserveLabel_NoAuthorino(t *testing.T) {
	// This test demonstrates the same bug in the second deletion loop (lines 311-326)
	// when authorino is NOT installed. The code deletes servicemesh resources based on
	// name/namespace alone without checking for Kserve OwnerReferences.

	ctx := t.Context()
	g := NewWithT(t)

	// Create an external EnvoyFilter resource WITHOUT Kserve OwnerReference.
	// This matches the name/namespace from the activator-envoyfilter.tmpl.yaml template.
	// This simulates an EnvoyFilter that was created by the user or another controller.
	externalEnvoyFilter := resources.GvkToUnstructured(gvk.EnvoyFilter)
	externalEnvoyFilter.SetName("activator-host-header")
	externalEnvoyFilter.SetNamespace("istio-system") // Matches ControlPlane.Namespace
	// Intentionally not setting Kserve OwnerReference
	externalEnvoyFilter.SetLabels(map[string]string{
		"external-resource": "true",
	})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
			// NO authorino-operator Subscription - this triggers the line 311 code path
			externalEnvoyFilter,
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed
	ks.Spec.Serving.Name = "knative-serving"

	// ServiceMesh set to Managed (not Removed) to avoid the first deletion loop at line 288
	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ApplicationsNamespace = "opendatahub"
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
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

	// Add template files which will generate an EnvoyFilter resource in rr.Resources.
	// This template resource will have the platform.opendatahub.io/dependency: servicemesh label.
	err = addTemplateFiles(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = template.NewAction(
		template.WithDataFn(getTemplateData),
	)(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify the external EnvoyFilter exists before cleanup
	envoyFilterBefore := resources.GvkToUnstructured(gvk.EnvoyFilter)
	err = cli.Get(ctx, client.ObjectKey{Name: "activator-host-header", Namespace: "istio-system"}, envoyFilterBefore)
	g.Expect(err).ShouldNot(HaveOccurred(), "EnvoyFilter should exist before cleanup")

	// Execute the cleanup function
	err = cleanUpTemplatedResources(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// BUG: The external EnvoyFilter resource gets deleted even though it doesn't have
	// a Kserve OwnerReference.
	//
	// This happens on line 314 of kserve_controller_actions.go in the second deletion loop.
	// When authorino is NOT installed (!authorinoInstalled), the code:
	// 1. Iterates through rr.Resources (which contains the template resource with dependency: servicemesh label)
	// 2. For each resource matching isForDependency("servicemesh"), it calls rr.Client.Delete(&res, ...)
	// 3. The delete uses the name/namespace from the template resource (&res)
	// 4. This deletes ANY cluster resource with that name/namespace, regardless of OwnerReferences
	//
	// The fix should be the same as for the first deletion loop: fetch the cluster resource
	// first and check if it has a Kserve OwnerReference before deleting.
	envoyFilterAfter := resources.GvkToUnstructured(gvk.EnvoyFilter)
	err = cli.Get(ctx, client.ObjectKey{Name: "activator-host-header", Namespace: "istio-system"}, envoyFilterAfter)
	g.Expect(err).Should(HaveOccurred(), "EnvoyFilter should have been deleted (demonstrates the bug)")
	g.Expect(err.Error()).Should(ContainSubstring("not found"))
}
