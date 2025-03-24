package deployments_test

import (
	"context"
	"strings"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega"
)

func TestDeploymentsAvailableActionNotReady(t *testing.T) {
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
					labels.PlatformPartOf: ns,
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
					labels.PlatformPartOf: ns,
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := deployments.NewAction(
		deployments.WithSelectorLabel(labels.PlatformPartOf, ns))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
			}),
		),
	)
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionDeploymentsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
				"Reason": Equal(status.ConditionDeploymentsNotAvailableReason),
			}),
		),
	)
}

func TestDeploymentsAvailableActionReady(t *testing.T) {
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
					labels.PlatformPartOf: ns,
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
					labels.PlatformPartOf: ns,
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := deployments.NewAction(
		deployments.WithSelectorLabel(labels.PlatformPartOf, ns))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionDeploymentsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestDeploymentsAvailableReadyAutoSelector(t *testing.T) {
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
					labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
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
					labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := deployments.NewAction()

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionDeploymentsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestDeploymentsAvailableActionNotReadyNotFound(t *testing.T) {
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
					labels.PlatformPartOf: ns,
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
					labels.PlatformPartOf: ns,
				},
			},
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	action := deployments.NewAction()

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Dashboard{},
		DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}

	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionTypeReady),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
			}),
		),
	)
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionDeploymentsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
				"Reason": Equal(status.ConditionDeploymentsNotAvailableReason),
			}),
		),
	)
}
