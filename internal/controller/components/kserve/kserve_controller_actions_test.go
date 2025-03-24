//nolint:testpackage
package kserve

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_ServerlessUnmanaged(t *testing.T) {
	ctx := context.Background()
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
	ctx := context.Background()
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
		MatchError(ContainSubstring(status.ServiceMeshNotConfiguredMessage)),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoOperators(t *testing.T) {
	ctx := context.Background()
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
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
			Name: serviceMeshOperator,
		}},
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
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
			Name: serverlessOperator,
		}},
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
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
			Name: serviceMeshOperator,
		}},
		&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
			Name: serverlessOperator,
		}},
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
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionTrue),
		)),
	)
}

func TestCheckPreConditions_RawServiceConfig(t *testing.T) {
	ctx := context.Background()
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
