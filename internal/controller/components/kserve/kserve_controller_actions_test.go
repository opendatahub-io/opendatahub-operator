//nolint:testpackage
package kserve

import (
	"testing"

	"github.com/onsi/gomega/gstruct"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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
	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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

	dsci := dsciv2.DSCInitialization{}
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
