package deleteresource_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deleteresource"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestDeleteResourcesAction(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New(
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
					labels.K8SCommon.PartOf: "baz",
				},
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := deleteresource.NewAction(
		deleteresource.WithDeleteResourcesTypes(&appsv1.Deployment{}),
		deleteresource.WithDeleteResourcesLabel(labels.K8SCommon.PartOf, "foo"))

	err = action(ctx, &types.ReconciliationRequest{
		Client:   cl,
		Instance: nil,
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		Release:  common.Release{Name: cluster.OpenDataHub},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	deployments := appsv1.DeploymentList{}
	err = cl.List(ctx, &deployments)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(deployments.Items).Should(HaveLen(1))
	g.Expect(deployments.Items[0]).To(
		gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ObjectMeta": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Name": Equal("my-deployment-2"),
			}),
		}),
	)
}
