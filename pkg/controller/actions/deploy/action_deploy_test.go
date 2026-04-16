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
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestDeployAction(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
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
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
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

	ctx := t.Context()
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

	cl, err := fakeclient.New(fakeclient.WithObjects(oldObj))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
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

	ctx := t.Context()
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

func TestDeployErrorFormat(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()
	name := xid.New().String()

	// Use an unsupported deploy mode to force a deploy error and exercise the
	// error message format introduced in run().
	action := deploy.NewAction(
		deploy.WithMode(deploy.Mode("invalid")),
	)

	obj, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
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
		Resources: []unstructured.Unstructured{*obj},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("failure deploying resource " + ns + "/" + name))
}

func TestDeployDeOwn(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()
	name := xid.New().String()

	action := deploy.NewAction(
		// fake client does not yet support SSA
		// - https://github.com/kubernetes/kubernetes/issues/115598
		// - https://github.com/kubernetes-sigs/controller-runtime/issues/2341
		deploy.WithMode(deploy.ModePatch),
	)

	ref := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}

	cl, err := fakeclient.New(fakeclient.WithObjects())
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(true)
		}),
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
	}

	err = rr.AddResources(&ref)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm1 := resources.GvkToUnstructured(gvk.ConfigMap)
	cm1.SetNamespace(ref.Namespace)
	cm1.SetName(ref.Name)

	err = cl.Get(ctx, client.ObjectKeyFromObject(cm1), cm1)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(cm1).Should(And(
		jq.Match(`.metadata.annotations | has("%s") | not`, annotations.ManagedByODHOperator),
		jq.Match(`.metadata.ownerReferences | length == 1`),
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.Dashboard.Kind),
	))

	unmanaged := ref.DeepCopy()
	unmanaged.Annotations = map[string]string{
		annotations.ManagedByODHOperator: "false",
	}

	err = cl.Update(ctx, unmanaged)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm2 := resources.GvkToUnstructured(gvk.ConfigMap)
	cm2.SetNamespace(ref.Namespace)
	cm2.SetName(ref.Name)

	err = cl.Get(ctx, client.ObjectKeyFromObject(cm1), cm2)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(cm2).Should(And(
		jq.Match(`.metadata.annotations | has("%s") `, annotations.ManagedByODHOperator),
		jq.Match(`.metadata.ownerReferences | length == 0`),
	))
}

func setupManagedAnnotationTest(t *testing.T, managed string, replicas *int32, containers []corev1.Container) (client.Client, types.ReconciliationRequest, string) {
	t.Helper()
	g := NewWithT(t)

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	cl := et.Client()
	ns := xid.New().String()
	g.Expect(cl.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())

	rr := types.ReconciliationRequest{
		Client:     cl,
		Controller: mocks.NewMockController(func(m *mocks.MockController) { m.On("Owns", mock.Anything).Return(true) }),
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name:       componentApi.DashboardInstanceName,
				UID:        apimachinery.UID(xid.New().String()),
				Generation: 1,
			},
		},
		Release: common.Release{Name: cluster.OpenDataHub, Version: version.OperatorVersion{Version: semver.Version{Major: 1, Minor: 2, Patch: 3}}},
	}

	g.Expect(rr.AddResources(&appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-deployment", Namespace: ns, Annotations: map[string]string{annotations.ManagedByODHOperator: managed}},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: containers},
			},
		},
	})).To(Succeed())

	return cl, rr, ns
}

func TestDeployWithManagedAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		mode        deploy.Mode
		managed     string
		userValue   int32
		finalValue  int32
		description string
	}{
		{"ssa mode managed=true", deploy.ModeSSA, "true", 5, 2, "reverts modifications"},
		{"ssa mode managed=false", deploy.ModeSSA, "false", 5, 5, "preserves modifications"},
		{"patch mode managed=true", deploy.ModePatch, "true", 5, 2, "reverts modifications"},
		{"patch mode managed=false", deploy.ModePatch, "false", 5, 5, "preserves modifications"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			replicas := int32(2)
			cl, rr, ns := setupManagedAnnotationTest(t, tt.managed, &replicas,
				[]corev1.Container{{Name: "test", Image: "test:v1"}})

			action := deploy.NewAction(deploy.WithMode(tt.mode))
			g.Expect(action(ctx, &rr)).To(Succeed())

			deployed := &appsv1.Deployment{}
			g.Expect(cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-deployment"}, deployed)).To(Succeed())
			g.Expect(*deployed.Spec.Replicas).To(Equal(int32(2)))

			deployed.Spec.Replicas = &tt.userValue
			g.Expect(cl.Update(ctx, deployed)).To(Succeed())
			g.Expect(action(ctx, &rr)).To(Succeed())

			g.Expect(cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-deployment"}, deployed)).To(Succeed())
			g.Expect(*deployed.Spec.Replicas).To(Equal(tt.finalValue), tt.description)
		})
	}

	t.Run("ssa managed=true resets resources to manifest values", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		replicas := int32(1)
		cl, rr, ns := setupManagedAnnotationTest(t, "true", &replicas,
			[]corev1.Container{{
				Name:  "test",
				Image: "test:v1",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			}})

		action := deploy.NewAction(deploy.WithMode(deploy.ModeSSA))
		g.Expect(action(ctx, &rr)).To(Succeed())

		deployed := &appsv1.Deployment{}
		g.Expect(cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-deployment"}, deployed)).To(Succeed())
		deployed.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
				corev1.ResourceCPU:    resource.MustParse("250m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("128Mi"),
				corev1.ResourceCPU:    resource.MustParse("500m"),
			},
		}
		g.Expect(cl.Update(ctx, deployed)).To(Succeed())

		g.Expect(action(ctx, &rr)).To(Succeed())

		g.Expect(cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-deployment"}, deployed)).To(Succeed())
		g.Expect(deployed.Spec.Template.Spec.Containers[0].Resources.Requests).To(Equal(
			corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}),
			"should have only manifest memory request, user-added cpu removed")
		g.Expect(deployed.Spec.Template.Spec.Containers[0].Resources.Limits).To(Equal(
			corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}),
			"should have only manifest memory limit, user-added cpu removed")
	})
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

	cli, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("aggregation", func(t *testing.T) {
		ctx := t.Context()
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
		ctx := t.Context()
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

func deployClusterRoles(t *testing.T, ctx context.Context, cli client.Client, roles ...rbacv1.ClusterRole) {
	t.Helper()

	g := NewWithT(t)

	rr := types.ReconciliationRequest{
		Client: cli,
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
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
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

	ctx := t.Context()
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

	cli, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
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

	ctx := t.Context()
	ns := xid.New().String()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(dscv2.AddToScheme(s))
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

	cli, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	err = cli.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ToNot(HaveOccurred())

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"}}
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
		Client:   cli,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}}},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", gvk.ConfigMap).Return(true)
		}),
	}

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

func TestDeployDynamicOwnership_SetsOwnerReferences(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create namespace for resources
	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create the owner instance
	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	// Resources to deploy
	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	secret, err := resources.ToUnstructured(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*configMap, *secret},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			// Simulate dynamic ownership enabled
			m.On("IsDynamicOwnershipEnabled").Return(true)
			// No GVKs are excluded
			m.On("IsExcludedFromDynamicOwnership", mock.Anything).Return(false)
		}),
	}

	action := deploy.NewAction()
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ConfigMap has owner reference
	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.GetOwnerReferences()).Should(HaveLen(1))
	g.Expect(cm.GetOwnerReferences()[0].Kind).Should(Equal(instance.GroupVersionKind().Kind))

	// Verify Secret has owner reference
	sec := resources.GvkToUnstructured(gvk.Secret)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-secret"}, sec)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(sec.GetOwnerReferences()).Should(HaveLen(1))
	g.Expect(sec.GetOwnerReferences()[0].Kind).Should(Equal(instance.GroupVersionKind().Kind))
}

func TestDeployDynamicOwnership_ExcludesGVKs(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create namespace
	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	// ConfigMap will be owned, Secret will be excluded
	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	secret, err := resources.ToUnstructured(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*configMap, *secret},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			// Exclude Secret from ownership
			m.On("IsExcludedFromDynamicOwnership", gvk.Secret).Return(true)
			m.On("IsExcludedFromDynamicOwnership", gvk.ConfigMap).Return(false)
		}),
	}

	action := deploy.NewAction()
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ConfigMap has owner reference (not excluded)
	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.GetOwnerReferences()).Should(HaveLen(1))

	// Verify Secret does NOT have owner reference (excluded)
	sec := resources.GvkToUnstructured(gvk.Secret)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-secret"}, sec)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(sec.GetOwnerReferences()).Should(BeEmpty(), "Excluded GVK should not have owner reference")
}

func TestDeployDynamicOwnership_FallsBackToStaticOwnership(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	// ConfigMap is statically owned, Secret is not
	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	secret, err := resources.ToUnstructured(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*configMap, *secret},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			// Dynamic ownership disabled
			m.On("IsDynamicOwnershipEnabled").Return(false)
			m.On("IsExcludedFromDynamicOwnership", mock.Anything).Return(false)
			// Static ownership: ConfigMap is owned, Secret is not
			m.On("Owns", gvk.ConfigMap).Return(true)
			m.On("Owns", gvk.Secret).Return(false)
		}),
	}

	action := deploy.NewAction()
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ConfigMap has owner reference (statically owned)
	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.GetOwnerReferences()).Should(HaveLen(1))

	// Verify Secret does NOT have owner reference (not statically owned)
	sec := resources.GvkToUnstructured(gvk.Secret)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-secret"}, sec)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(sec.GetOwnerReferences()).Should(BeEmpty(), "Non-owned GVK should not have owner reference")
}

func TestDeployStripsTemplateOwnerReferences(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	// Simulate a template-rendered ConfigMap that already has ownerReferences
	templateOwnerRef := metav1.OwnerReference{
		APIVersion: gvk.Dashboard.GroupVersion().String(),
		Kind:       gvk.Dashboard.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
	}

	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cm",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{templateOwnerRef},
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*configMap},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", gvk.ConfigMap).Return(true)
		}),
	}

	action := deploy.NewAction(deploy.WithMode(deploy.ModePatch))
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify exactly one ownerReference set by SetControllerReference
	// (with controller=true), NOT the template-defined one (without controller field).
	ownerRefs := cm.GetOwnerReferences()
	g.Expect(ownerRefs).Should(HaveLen(1))
	g.Expect(ownerRefs[0].Kind).Should(Equal(gvk.Dashboard.Kind))
	g.Expect(ownerRefs[0].Name).Should(Equal(instance.Name))
	g.Expect(ownerRefs[0].UID).Should(Equal(instance.UID))
	g.Expect(*ownerRefs[0].Controller).Should(BeTrue(),
		"ownerReference should be a controller reference set by SetControllerReference, not a template-defined one")
	g.Expect(*ownerRefs[0].BlockOwnerDeletion).Should(BeTrue())
}

func TestDeployStripsTemplateOwnerReferences_RepeatedReconciliation(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	templateOwnerRef := metav1.OwnerReference{
		APIVersion: gvk.Dashboard.GroupVersion().String(),
		Kind:       gvk.Dashboard.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
	}

	action := deploy.NewAction(deploy.WithMode(deploy.ModePatch))

	// Run two reconciliation cycles with the same template ownerRef present each time.
	// This proves repeated reconciliations don't accumulate ownerRefs or cause drift.
	for i := range 2 {
		configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            "test-cm",
				Namespace:       ns,
				OwnerReferences: []metav1.OwnerReference{templateOwnerRef},
			},
		})
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := types.ReconciliationRequest{
			Client:   cl,
			Instance: instance,
			Release: common.Release{
				Name: cluster.OpenDataHub,
				Version: version.OperatorVersion{Version: semver.Version{
					Major: 1, Minor: 2, Patch: 3,
				}},
			},
			Resources: []unstructured.Unstructured{*configMap},
			Controller: mocks.NewMockController(func(m *mocks.MockController) {
				m.On("Owns", gvk.ConfigMap).Return(true)
			}),
		}

		err = action(ctx, &rr)
		g.Expect(err).ShouldNot(HaveOccurred(), "reconciliation %d should not fail", i+1)

		cm := resources.GvkToUnstructured(gvk.ConfigMap)
		err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
		g.Expect(err).ShouldNot(HaveOccurred())

		ownerRefs := cm.GetOwnerReferences()
		g.Expect(ownerRefs).Should(HaveLen(1),
			"reconciliation %d: should have exactly one ownerReference, got %d", i+1, len(ownerRefs))
		g.Expect(ownerRefs[0].Kind).Should(Equal(gvk.Dashboard.Kind))
		g.Expect(*ownerRefs[0].Controller).Should(BeTrue(),
			"reconciliation %d: ownerReference should be a controller reference", i+1)
	}
}

func TestDeployStripsTemplateOwnerReferences_ForeignOwner(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "dashboard-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	// Template ownerRef pointing to a different resource (DataScienceCluster),
	// simulating the actual bug where a template ownerRef points to a parent
	// resource rather than the component controller.
	foreignOwnerRef := metav1.OwnerReference{
		APIVersion: gvk.DataScienceCluster.GroupVersion().String(),
		Kind:       gvk.DataScienceCluster.Kind,
		Name:       "default-dsc",
		UID:        "dsc-uid-99999",
	}

	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cm",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{foreignOwnerRef},
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*configMap},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", gvk.ConfigMap).Return(true)
		}),
	}

	action := deploy.NewAction(deploy.WithMode(deploy.ModePatch))
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())

	// The foreign DSC ownerRef must be replaced by the controller reference
	// pointing to the Dashboard instance.
	ownerRefs := cm.GetOwnerReferences()
	g.Expect(ownerRefs).Should(HaveLen(1))
	g.Expect(ownerRefs[0].Kind).Should(Equal(gvk.Dashboard.Kind),
		"ownerReference should point to Dashboard, not DataScienceCluster")
	g.Expect(ownerRefs[0].Name).Should(Equal(instance.Name))
	g.Expect(ownerRefs[0].UID).Should(Equal(instance.UID))
	g.Expect(*ownerRefs[0].Controller).Should(BeTrue())
	g.Expect(*ownerRefs[0].BlockOwnerDeletion).Should(BeTrue())
}

func TestDeployStripsTemplateOwnerReferences_SSAMode(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	cl := et.Client()
	ns := xid.New().String()
	g.Expect(cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	g.Expect(cl.Create(ctx, instance)).To(Succeed())

	// Template ownerRef that should be cleared and replaced by SetControllerReference
	templateOwnerRef := metav1.OwnerReference{
		APIVersion: gvk.Dashboard.GroupVersion().String(),
		Kind:       gvk.Dashboard.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
	}

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", gvk.ConfigMap).Return(true)
		}),
	}

	g.Expect(rr.AddResources(&corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: ns, OwnerReferences: []metav1.OwnerReference{templateOwnerRef}},
	})).To(Succeed())

	// Default mode is SSA
	action := deploy.NewAction()
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := &corev1.ConfigMap{}
	g.Expect(cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)).To(Succeed())

	ownerRefs := cm.GetOwnerReferences()
	g.Expect(ownerRefs).Should(HaveLen(1))
	g.Expect(ownerRefs[0].Kind).Should(Equal(gvk.Dashboard.Kind))
	g.Expect(ownerRefs[0].Name).Should(Equal(instance.Name))
	g.Expect(ownerRefs[0].UID).Should(Equal(instance.UID))
	g.Expect(*ownerRefs[0].Controller).Should(BeTrue(),
		"ownerReference should be a controller reference set by SetControllerReference, not a template-defined one")
	g.Expect(*ownerRefs[0].BlockOwnerDeletion).Should(BeTrue())
}

func TestDeployDynamicOwnership_CRDsExcludedByDefault(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	g.Expect(err).ShouldNot(HaveOccurred())

	instance := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:       componentApi.DashboardInstanceName,
			UID:        "test-uid-12345",
			Generation: 1,
		},
	}
	instance.SetGroupVersionKind(gvk.Dashboard)

	configMap, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	crd, err := resources.ToUnstructured(&apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testresources.test.opendatahub.io",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "test.opendatahub.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "TestResource",
				ListKind: "TestResourceList",
				Plural:   "testresources",
				Singular: "testresource",
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
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: instance,
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*crd, *configMap},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromDynamicOwnership", mock.Anything).Return(false)
		}),
	}

	action := deploy.NewAction()
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify CRD does NOT have owner reference
	obj := resources.GvkToUnstructured(gvk.CustomResourceDefinition)
	err = cl.Get(ctx, apimachinery.NamespacedName{Name: "testresources.test.opendatahub.io"}, obj)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(obj.GetOwnerReferences()).Should(BeEmpty(), "CRDs should not have owner references")

	// Verify ConfigMap has owner reference
	cm := resources.GvkToUnstructured(gvk.ConfigMap)
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "test-cm"}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cm.GetOwnerReferences()).Should(HaveLen(1))
	g.Expect(cm.GetOwnerReferences()[0].Kind).Should(Equal(instance.GroupVersionKind().Kind))
}

func TestWithSortFn(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	var sortCalled bool
	var sortInput []string

	action := deploy.NewAction(
		deploy.WithSortFn(func(ctx context.Context, res []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
			sortCalled = true
			for _, r := range res {
				sortInput = append(sortInput, r.GetKind())
			}
			// Reverse the order to prove sorting was applied
			reversed := make([]unstructured.Unstructured, len(res))
			for i, r := range res {
				reversed[len(res)-1-i] = r
			}
			return reversed, nil
		}),
	)

	obj1, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm-1",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	obj2, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       gvk.Deployment.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-1",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*obj1, *obj2},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(sortCalled).To(BeTrue(), "sort function should have been called")
	g.Expect(sortInput).To(Equal([]string{gvk.ConfigMap.Kind, gvk.Deployment.Kind}))

	// Verify resources were reordered (reversed by our sort fn)
	g.Expect(rr.Resources[0].GetKind()).To(Equal(gvk.Deployment.Kind))
	g.Expect(rr.Resources[1].GetKind()).To(Equal(gvk.ConfigMap.Kind))

	// Verify both resources were deployed
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "cm-1"}, resources.GvkToUnstructured(gvk.ConfigMap))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "deploy-1"}, resources.GvkToUnstructured(gvk.Deployment))
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestWithApplyOrder(t *testing.T) {
	g := NewWithT(t)

	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := deploy.NewAction(
		deploy.WithApplyOrder(),
	)

	obj1, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-1",
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	obj2, err := resources.ToUnstructured(&corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns-" + xid.New().String(),
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	// Input order: Deployment, Namespace (wrong order)
	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{Version: semver.Version{
				Major: 1, Minor: 2, Patch: 3,
			}},
		},
		Resources: []unstructured.Unstructured{*obj1, *obj2},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify resources were reordered: Namespace before Deployment
	g.Expect(rr.Resources[0].GetKind()).To(Equal(gvk.Namespace.Kind))
	g.Expect(rr.Resources[1].GetKind()).To(Equal(gvk.Deployment.Kind))

	// Verify both resources were deployed
	err = cl.Get(ctx, apimachinery.NamespacedName{Namespace: ns, Name: "deploy-1"}, resources.GvkToUnstructured(gvk.Deployment))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cl.Get(ctx, apimachinery.NamespacedName{Name: obj2.GetName()}, resources.GvkToUnstructured(gvk.Namespace))
	g.Expect(err).ShouldNot(HaveOccurred())
}
