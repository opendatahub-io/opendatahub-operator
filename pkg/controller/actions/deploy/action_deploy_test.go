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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	odhCli "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/manager"
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
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
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
		jq.Match(`.metadata.labels."%s" == "%s"`, labels.PlatformPartOf, strings.ToLower(componentApi.DashboardKind)),
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
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
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
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
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
	utilruntime.Must(componentApi.AddToScheme(s))
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
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				UID:        apimachinery.UID(xid.New().String()),
			},
		},
		Release: common.Release{
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

func TestDeployCRD(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	ctx := context.Background()
	id := xid.New().String()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
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

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: id,
		}},
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				UID:        apimachinery.UID(id),
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
	}

	err = rr.AddResources(&apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "acceleratorprofiles.dashboard.opendatahub.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "dashboard.opendatahub.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "AcceleratorProfile",
				ListKind: "AcceleratorProfileList",
				Plural:   "acceleratorprofiles",
				Singular: "acceleratorprofile",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())

	err = deploy.NewAction()(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	out := resources.GvkToUnstructured(gvk.CustomResourceDefinition)
	out.SetName("acceleratorprofiles.dashboard.opendatahub.io")

	err = cli.Get(ctx, client.ObjectKeyFromObject(out), out)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(out).Should(And(
		jq.Match(`.metadata.labels."%s" == "%s"`, labels.PlatformPartOf, labels.Platform),
		Not(jq.Match(`.metadata | has ("annotations")`)),
	))
}

func TestDeployOwnerRef(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	ctx := context.Background()
	id := xid.New().String()
	ns := xid.New().String()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
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

	err = cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ToNot(HaveOccurred())

	dsc := &dscv1.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"}}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)

	err = cli.Create(ctx, dsc)
	g.Expect(err).ToNot(HaveOccurred())

	instance := &componentApi.Dashboard{ObjectMeta: metav1.ObjectMeta{Name: componentApi.DashboardInstanceName}}
	instance.SetGroupVersionKind(gvk.Dashboard)

	err = cli.Create(ctx, instance)
	g.Expect(err).ToNot(HaveOccurred())

	//
	// ConfigMap
	//

	configMapRef := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: ns}}
	configMapRef.SetGroupVersionKind(gvk.ConfigMap)

	configMap := configMapRef.DeepCopy()
	err = controllerutil.SetOwnerReference(dsc, configMap, s)
	g.Expect(err).ToNot(HaveOccurred())

	err = cli.Create(ctx, configMap.DeepCopy())
	g.Expect(err).ToNot(HaveOccurred())

	//
	// CustomResourceDefinition
	//

	crdRef := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "acceleratorprofiles.dashboard.opendatahub.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "dashboard.opendatahub.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "AcceleratorProfile",
				ListKind: "AcceleratorProfileList",
				Plural:   "acceleratorprofiles",
				Singular: "acceleratorprofile",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}

	crdRef.SetGroupVersionKind(gvk.CustomResourceDefinition)

	crd := crdRef.DeepCopy()
	err = controllerutil.SetOwnerReference(dsc, crd, s)
	g.Expect(err).ToNot(HaveOccurred())

	err = cli.Create(ctx, crd.DeepCopy())
	g.Expect(err).ToNot(HaveOccurred())

	//
	// deploy
	//

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: id,
		}},
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Manager: manager.New(nil),
	}

	rr.Manager.AddGVK(gvk.ConfigMap, true)

	err = rr.AddResources(configMapRef.DeepCopy(), crdRef.DeepCopy())
	g.Expect(err).NotTo(HaveOccurred())

	err = deploy.NewAction()(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	updatedConfigMap := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKeyFromObject(configMapRef), updatedConfigMap)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(updatedConfigMap.GetOwnerReferences()).Should(And(
		HaveLen(1),
		HaveEach(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":       Equal(instance.Name),
			"APIVersion": Equal(gvk.Dashboard.GroupVersion().String()),
			"Kind":       Equal(gvk.Dashboard.Kind),
			"UID":        Equal(instance.UID),
		})),
	))

	updatedCRD := &apiextensionsv1.CustomResourceDefinition{}
	err = cli.Get(ctx, client.ObjectKeyFromObject(crdRef), updatedCRD)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(updatedCRD.GetOwnerReferences()).Should(BeEmpty())
}
