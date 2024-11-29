package deploy_test

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/onsi/gomega/gstruct"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apimachinery "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	odhCli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

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

	g.Expect(obj1).Should(And(
		jq.Match(`.metadata.labels."%s" == "%s"`, labels.ComponentPartOf, strings.ToLower(componentsv1.DashboardKind)),
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.InstanceGeneration, strconv.FormatInt(rr.Instance.GetGeneration(), 10)),
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

func TestDeployClusterRole(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentsv1.AddToScheme(s))
	utilruntime.Must(rbacv1.AddToScheme(s))

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	envTest := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := odhCli.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("aggregation", func(t *testing.T) {
		ctx := context.Background()
		name := xid.New().String()

		deployClusterRoles(t, ctx, cli, rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: []rbacv1.PolicyRule{{
				Verbs:     []string{"*"},
				Resources: []string{"*"},
				APIGroups: []string{"*"},
			}},
			AggregationRule: &rbacv1.AggregationRule{
				ClusterRoleSelectors: []metav1.LabelSelector{{
					MatchLabels: map[string]string{"foo": "bar"},
				}},
			},
		})

		out := rbacv1.ClusterRole{}
		err = cli.Get(ctx, client.ObjectKey{Name: name}, &out)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(out).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Rules": BeEmpty(),
		}))
	})

	t.Run("no aggregation", func(t *testing.T) {
		ctx := context.Background()
		name := xid.New().String()

		deployClusterRoles(t, ctx, cli, rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: []rbacv1.PolicyRule{{
				Verbs:     []string{"*"},
				Resources: []string{"*"},
				APIGroups: []string{"*"},
			}},
		})

		out := rbacv1.ClusterRole{}
		err = cli.Get(ctx, client.ObjectKey{Name: name}, &out)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(out).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Rules": HaveLen(1),
		}))
	})
}

func deployClusterRoles(t *testing.T, ctx context.Context, cli *odhCli.Client, roles ...rbacv1.ClusterRole) {
	t.Helper()

	g := NewWithT(t)

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: xid.New().String(),
		}},
		DSC: &dscv1.DataScienceCluster{},
		Instance: &componentsv1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				UID:        apimachinery.UID(xid.New().String()),
			},
		},
		Release: cluster.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
	}

	for i := range roles {
		err := rr.AddResources(roles[i].DeepCopy())
		g.Expect(err).ShouldNot(HaveOccurred())
	}

	err := deploy.NewAction()(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}
