package deploy_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestDeployAction(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := deploy.NewAction(
		// fake client does not yet support SSA
		// - https://github.com/kubernetes/kubernetes/issues/115598
		// - https://github.com/kubernetes-sigs/controller-runtime/issues/2341
		deploy.WithMode(deploy.ModePatch),
	)

	obj1, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      xid.New().String(),
			Namespace: ns,
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
		DSCI:   &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:    &dscv1.DataScienceCluster{},
		Instance: &componentsv1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: cluster.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{*obj1},
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Get(ctx, client.ObjectKeyFromObject(obj1), obj1)
	g.Expect(err).ShouldNot(HaveOccurred())

	dh, err := types.HashStr(&rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(obj1).Should(And(
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.ComponentGeneration, strconv.FormatInt(rr.Instance.GetGeneration(), 10)),
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.ComponentHash, dh),
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformVersion, "1.2.3"),
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, string(cluster.OpenDataHub)),
	))
}

func TestDeployNotOwnedSkip(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	name := xid.New().String()

	action := deploy.NewAction(
		// fake client does not yet support SSA
		// - https://github.com/kubernetes/kubernetes/issues/115598
		// - https://github.com/kubernetes-sigs/controller-runtime/issues/2341
		deploy.WithMode(deploy.ModePatch),
	)

	oldObj, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	newObj, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Annotations: map[string]string{
				annotations.ManagedByODHOperator: "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	cl, err := fakeclient.New(oldObj)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
		DSCI:   &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:    &dscv1.DataScienceCluster{},
		Instance: &componentsv1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: cluster.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{*newObj},
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Get(ctx, client.ObjectKeyFromObject(newObj), newObj)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(newObj).Should(And(
		jq.Match(`.metadata.annotations | has("%s") | not`, annotations.ManagedByODHOperator),
		jq.Match(`.spec.strategy.type == "%s"`, appsv1.RecreateDeploymentStrategyType),
	))
}

func TestDeployNotOwnedCreate(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	name := xid.New().String()

	action := deploy.NewAction(
		// fake client does not yet support SSA
		// - https://github.com/kubernetes/kubernetes/issues/115598
		// - https://github.com/kubernetes-sigs/controller-runtime/issues/2341
		deploy.WithMode(deploy.ModePatch),
	)

	newObj, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Annotations: map[string]string{
				annotations.ManagedByODHOperator: "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
		DSCI:   &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
		DSC:    &dscv1.DataScienceCluster{},
		Instance: &componentsv1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: cluster.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Resources: []unstructured.Unstructured{*newObj},
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Get(ctx, client.ObjectKeyFromObject(newObj), newObj)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(newObj).Should(And(
		jq.Match(`.metadata.annotations | has("%s") | not`, annotations.ManagedByODHOperator),
		jq.Match(`.spec.strategy.type == "%s"`, appsv1.RollingUpdateDeploymentStrategyType),
	))
}
