//nolint:dupl
package updatestatus_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/updatestatus"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega"
)

//nolint:dupl
func TestUpdateStatusActionNotReady(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New(
		ctx,
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Deployment.GroupVersion().String(),
				Kind:       gvk.Deployment.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deployment",
				Namespace: ns,
				Labels: map[string]string{
					labels.K8SCommon.PartOf: "foo",
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 0,
			},
		},
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Deployment.GroupVersion().String(),
				Kind:       gvk.Deployment.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deployment-2",
				Namespace: ns,
				Labels: map[string]string{
					labels.K8SCommon.PartOf: "foo",
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := updatestatus.NewAction(
		updatestatus.WithSelectorLabel(labels.K8SCommon.PartOf, "foo"))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentsv1.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:      &dscv1.DataScienceCluster{},
		Release:  cluster.Release{Name: cluster.OpenDataHub},
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
				"Reason": Equal(updatestatus.DeploymentsNotReadyReason),
			}),
		),
	)
}

func TestUpdateStatusActionReady(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New(
		ctx,
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Deployment.GroupVersion().String(),
				Kind:       gvk.Deployment.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deployment",
				Namespace: ns,
				Labels: map[string]string{
					labels.K8SCommon.PartOf: "foo",
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvk.Deployment.GroupVersion().String(),
				Kind:       gvk.Deployment.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deployment-2",
				Namespace: ns,
				Labels: map[string]string{
					labels.K8SCommon.PartOf: "foo",
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := updatestatus.NewAction(
		updatestatus.WithSelectorLabel(labels.K8SCommon.PartOf, "foo"))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentsv1.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:      &dscv1.DataScienceCluster{},
		Release:  cluster.Release{Name: cluster.OpenDataHub},
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
				"Reason": Equal(updatestatus.ReadyReason),
			}),
		),
	)
}
