package components_test

import (
	"context"
	"github.com/onsi/gomega/gstruct"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"

	. "github.com/onsi/gomega"
)

func NewFakeClient(scheme *runtime.Scheme, objs ...ctrlClient.Object) ctrlClient.WithWatch {
	fakeMapper := meta.NewDefaultRESTMapper(scheme.PreferredVersionAllGroups())
	for gvk := range scheme.AllKnownTypes() {
		switch {
		// TODO: add cases for cluster scoped
		default:
			fakeMapper.Add(gvk, meta.RESTScopeNamespace)
		}
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(fakeMapper).
		WithObjects(objs...).
		Build()
}

func TestDeleteResourcesAction(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))

	client := NewFakeClient(
		scheme,
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
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
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
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

	action := components.NewDeleteResourcesAction(
		ctx,
		components.WithDeleteResourcesTypes(&appsv1.Deployment{}),
		components.WithDeleteResourcesLabel(labels.K8SCommon.PartOf, "foo"))

	err := action.Execute(ctx, &components.ReconciliationRequest{
		Client:   client,
		Instance: nil,
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:      &dscv1.DataScienceCluster{},
		Platform: cluster.OpenDataHub,
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	deployments := appsv1.DeploymentList{}
	err = client.List(ctx, &deployments)

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
