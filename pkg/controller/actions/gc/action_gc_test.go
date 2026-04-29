package gc_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	gTypes "github.com/onsi/gomega/types"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

var sharedEnvTest *envt.EnvT

func TestMain(m *testing.M) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	var err error

	sharedEnvTest, err = envt.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := sharedEnvTest.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to stop envtest: %v\n", err)
	}

	os.Exit(code)
}

//nolint:maintidx
func TestGcAction(t *testing.T) {
	ctx := context.Background()
	cli := sharedEnvTest.Client()

	tests := []struct {
		name           string
		version        semver.Version
		generated      bool
		matcher        gTypes.GomegaMatcher
		metricsMatcher gTypes.GomegaMatcher
		labels         map[string]string
		annotations    map[string]string
		options        []gc.ActionOpts
		uidFn          func(request *types.ReconciliationRequest) string
	}{
		{
			name:           "should delete leftovers",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Satisfy(k8serr.IsNotFound),
			metricsMatcher: BeNumerically("==", 1),
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because same annotations",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because unmanaged",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			annotations:    map[string]string{annotations.ManagedByODHOperator: "false"},
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because of no generated resources have been detected",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      false,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 0),
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because of selector",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			labels:         map[string]string{"foo": "bar"},
			options:        []gc.ActionOpts{gc.WithLabel("foo", "baz")},
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because of unremovable type",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			options:        []gc.ActionOpts{gc.WithUnremovables(gvk.ConfigMap)},
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should not delete resources because of predicate",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			options: []gc.ActionOpts{gc.WithObjectPredicate(
				func(request *types.ReconciliationRequest, unstructured unstructured.Unstructured) (bool, error) {
					return unstructured.GroupVersionKind() != gvk.ConfigMap, nil
				},
			)},
			uidFn: func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
		{
			name:           "should delete leftovers because of UID",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Satisfy(k8serr.IsNotFound),
			metricsMatcher: BeNumerically("==", 1),
			uidFn:          func(rr *types.ReconciliationRequest) string { return xid.New().String() },
		},
		{
			name:           "should not delete leftovers because of UID",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			uidFn:          func(rr *types.ReconciliationRequest) string { return string(rr.Instance.GetUID()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gc.CyclesTotal.Reset()
			gc.CyclesTotal.WithLabelValues("dashboard").Add(0)

			g := NewWithT(t)
			id := xid.New().String()
			nsn := xid.New().String()

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())

			rr := types.ReconciliationRequest{
				Client: cli,
				Instance: &componentApi.Dashboard{
					TypeMeta: metav1.TypeMeta{
						APIVersion: componentApi.GroupVersion.String(),
						Kind:       componentApi.DashboardKind,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: componentApi.DashboardInstanceName,
					},
				},
				Release: common.Release{
					Name: cluster.OpenDataHub,
					Version: version.OperatorVersion{
						Version: tt.version,
					},
				},
				Generated: tt.generated,
				Controller: mocks.NewMockController(func(m *mocks.MockController) {
					m.On("GetClient").Return(sharedEnvTest.Client())
					m.On("GetDynamicClient").Return(sharedEnvTest.DynamicClient())
					m.On("GetDiscoveryClient").Return(sharedEnvTest.DiscoveryClient())
					m.On("Owns", mock.Anything).Return(false)
				}),
			}

			g.Expect(cli.Create(ctx, rr.Instance)).
				NotTo(HaveOccurred())

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			// should never get deleted
			crd := apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foos." + id + ".opendatahub.io",
					Labels: map[string]string{
						labels.PlatformPartOf: labels.Platform,
					},
				},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Group: id + ".opendatahub.io",
					Names: apiextensionsv1.CustomResourceDefinitionNames{
						Kind:     "Foo",
						ListKind: "FooList",
						Plural:   "foos",
						Singular: "foo",
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

			t.Cleanup(func() {
				g.Eventually(func() error {
					return cli.Delete(ctx, &crd)
				}).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			g.Expect(cli.Create(ctx, &crd)).
				NotTo(HaveOccurred())

			commonAnnotations := map[string]string{
				annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
				annotations.InstanceUID:        tt.uidFn(&rr),
				annotations.PlatformVersion:    "0.1.0",
				annotations.PlatformType:       string(cluster.OpenDataHub),
			}

			commonLabels := map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			}

			// should never get deleted
			l := coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:        id,
					Namespace:   nsn,
					Annotations: maps.Clone(commonAnnotations),
					Labels:      maps.Clone(commonLabels),
				},
			}

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, &l)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			g.Expect(cli.Create(ctx, &l)).
				NotTo(HaveOccurred())

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "gc-cm",
					Namespace:   nsn,
					Annotations: maps.Clone(commonAnnotations),
					Labels:      maps.Clone(commonLabels),
				},
			}

			maps.Copy(cm.Labels, tt.labels)
			maps.Copy(cm.Annotations, tt.annotations)

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, &cm)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm, cli.Scheme())).
				NotTo(HaveOccurred())

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			opts := make([]gc.ActionOpts, 0, len(tt.options)+1)
			opts = append(opts, gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground))
			opts = append(opts, gc.InNamespace(nsn))
			opts = append(opts, tt.options...)

			a := gc.NewAction(opts...)

			g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())

			g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&l), &coordinationv1.Lease{})).
				ToNot(HaveOccurred())

			g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&crd), &apiextensionsv1.CustomResourceDefinition{})).
				ToNot(HaveOccurred())

			if tt.matcher != nil {
				g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm), &corev1.ConfigMap{})).
					To(tt.matcher)
			}

			if tt.metricsMatcher != nil {
				ct := testutil.ToFloat64(gc.CyclesTotal)
				g.Expect(ct).Should(tt.metricsMatcher)
			}
		})
	}
}

func TestGcActionOwn(t *testing.T) {
	ctx := context.Background()
	cli := sharedEnvTest.Client()

	tests := []struct {
		name    string
		matcher gTypes.GomegaMatcher
		options []gc.ActionOpts
		owned   bool
	}{
		{
			name:    "should delete owned resources",
			matcher: Satisfy(k8serr.IsNotFound),
			owned:   true,
		},
		{
			name:    "should not delete non owned resources",
			matcher: Not(HaveOccurred()),
			owned:   false,
		},
		{
			name:    "should delete non owned resources",
			matcher: Satisfy(k8serr.IsNotFound),
			owned:   true,
			options: []gc.ActionOpts{gc.WithOnlyCollectOwned(false)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gc.CyclesTotal.Reset()
			gc.CyclesTotal.WithLabelValues("dashboard").Add(0)

			g := NewWithT(t)
			nsn := xid.New().String()

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())

			rr := types.ReconciliationRequest{
				Client: cli,
				Instance: &componentApi.Dashboard{
					TypeMeta: metav1.TypeMeta{
						APIVersion: componentApi.GroupVersion.String(),
						Kind:       componentApi.DashboardKind,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: componentApi.DashboardInstanceName,
					},
				},
				Release: common.Release{
					Name: cluster.OpenDataHub,
					Version: version.OperatorVersion{
						Version: semver.Version{Major: 0, Minor: 0, Patch: 1},
					},
				},
				Generated: true,
				Controller: mocks.NewMockController(func(m *mocks.MockController) {
					m.On("GetClient").Return(sharedEnvTest.Client())
					m.On("GetDynamicClient").Return(sharedEnvTest.DynamicClient())
					m.On("GetDiscoveryClient").Return(sharedEnvTest.DiscoveryClient())
					m.On("Owns", mock.Anything).Return(false)
				}),
			}

			g.Expect(cli.Create(ctx, rr.Instance)).
				NotTo(HaveOccurred())

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gc-cm",
					Namespace: nsn,
					Annotations: map[string]string{
						annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
						annotations.InstanceUID:        xid.New().String(),
						annotations.PlatformVersion:    rr.Release.Version.String(),
						annotations.PlatformType:       string(rr.Release.Name),
					},
					Labels: map[string]string{
						labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
					},
				},
			}

			t.Cleanup(func() {
				g.Expect(cli.Delete(ctx, &cm)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			})

			if tt.owned {
				g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm, cli.Scheme())).
					NotTo(HaveOccurred())
			}

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			opts := make([]gc.ActionOpts, 0, len(tt.options)+1)
			opts = append(opts, gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground))
			opts = append(opts, gc.InNamespace(nsn))
			opts = append(opts, tt.options...)

			a := gc.NewAction(opts...)

			g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())

			if tt.matcher != nil {
				g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm), &corev1.ConfigMap{})).
					To(tt.matcher)
			}
		})
	}
}

func TestGcActionCluster(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	cli := sharedEnvTest.Client()
	nsn := xid.New().String()

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).
		NotTo(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		Instance: &componentApi.Dashboard{
			TypeMeta: metav1.TypeMeta{
				APIVersion: componentApi.GroupVersion.String(),
				Kind:       componentApi.DashboardKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DashboardInstanceName,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{
				Version: semver.Version{Major: 0, Minor: 2, Patch: 0},
			},
		},
		Generated: true,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("GetClient").Return(sharedEnvTest.Client())
			m.On("GetDynamicClient").Return(sharedEnvTest.DynamicClient())
			m.On("GetDiscoveryClient").Return(sharedEnvTest.DiscoveryClient())
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	g.Expect(cli.Create(ctx, rr.Instance)).
		NotTo(HaveOccurred())

	t.Cleanup(func() {
		g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	om := metav1.ObjectMeta{
		Namespace: nsn,
		Annotations: map[string]string{
			annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
			annotations.InstanceUID:        string(rr.Instance.GetUID()),
			annotations.PlatformType:       string(cluster.OpenDataHub),
		},
		Labels: map[string]string{
			labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
		},
	}

	cm1 := corev1.ConfigMap{ObjectMeta: *om.DeepCopy()}
	cm1.Name = xid.New().String()
	cm1.Annotations[annotations.PlatformVersion] = "0.1.0"

	cm2 := corev1.ConfigMap{ObjectMeta: *om.DeepCopy()}
	cm2.Name = xid.New().String()
	cm2.Annotations[annotations.PlatformVersion] = rr.Release.Version.String()

	cr1 := rbacv1.ClusterRole{ObjectMeta: *om.DeepCopy()}
	cr1.Name = xid.New().String()
	cr1.Annotations[annotations.PlatformVersion] = "0.1.0"

	cr2 := rbacv1.ClusterRole{ObjectMeta: *om.DeepCopy()}
	cr2.Name = xid.New().String()
	cr2.Annotations[annotations.PlatformVersion] = rr.Release.Version.String()

	t.Cleanup(func() {
		for _, obj := range []ctrlCli.Object{&cm1, &cm2, &cr1, &cr2} {
			if err := cli.Delete(ctx, obj); err != nil && !k8serr.IsNotFound(err) {
				t.Fatalf("failed to delete %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
			}
		}
	})

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm1, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cm1)).
		NotTo(HaveOccurred())

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm2, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cm2)).
		NotTo(HaveOccurred())

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cr1, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cr1)).
		NotTo(HaveOccurred())

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cr2, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cr2)).
		NotTo(HaveOccurred())

	a := gc.NewAction(gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground), gc.InNamespace(nsn))

	gc.DeletedTotal.Reset()
	gc.DeletedTotal.WithLabelValues("dashboard").Add(0)

	g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())

	g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm1), &corev1.ConfigMap{})).
		To(MatchError(k8serr.IsNotFound, "IsNotFound"))

	g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm2), &corev1.ConfigMap{})).
		ToNot(HaveOccurred())

	g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cr1), &rbacv1.ClusterRole{})).
		To(MatchError(k8serr.IsNotFound, "IsNotFound"))

	g.Expect(cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cr2), &rbacv1.ClusterRole{})).
		ToNot(HaveOccurred())

	ct := testutil.ToFloat64(gc.DeletedTotal)
	g.Expect(ct).Should(BeNumerically("==", 2))
}

func TestGcActionWithPartOfLabel(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()
	nsn := xid.New().String()
	customLabelKey := "custom.example.io/part-of"

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).NotTo(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		Instance: &componentApi.Dashboard{
			TypeMeta: metav1.TypeMeta{
				APIVersion: componentApi.GroupVersion.String(),
				Kind:       componentApi.DashboardKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DashboardInstanceName,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{
				Version: semver.Version{Major: 0, Minor: 1, Patch: 0},
			},
		},
		Generated: true,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("GetClient").Return(envTest.Client())
			m.On("GetDynamicClient").Return(envTest.DynamicClient())
			m.On("GetDiscoveryClient").Return(envTest.DiscoveryClient())
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	g.Expect(cli.Create(ctx, rr.Instance)).NotTo(HaveOccurred())

	t.Cleanup(func() {
		g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	staleAnnotations := map[string]string{
		annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
		annotations.InstanceUID:        xid.New().String(),
		annotations.PlatformVersion:    rr.Release.Version.String(),
		annotations.PlatformType:       string(cluster.OpenDataHub),
	}

	cmCustom := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "gc-cm-custom-label",
			Namespace:   nsn,
			Annotations: maps.Clone(staleAnnotations),
			Labels: map[string]string{
				customLabelKey: strings.ToLower(componentApi.DashboardKind),
			},
		},
	}

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cmCustom, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cmCustom)).NotTo(HaveOccurred())

	t.Cleanup(func() {
		g.Expect(cli.Delete(ctx, &cmCustom)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	cmDefault := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "gc-cm-default-label",
			Namespace:   nsn,
			Annotations: maps.Clone(staleAnnotations),
			Labels: map[string]string{
				labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
			},
		},
	}

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cmDefault, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cmDefault)).NotTo(HaveOccurred())

	t.Cleanup(func() {
		g.Expect(cli.Delete(ctx, &cmDefault)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	a := gc.NewAction(
		gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
		gc.InNamespace(nsn),
		gc.WithPartOfLabel(customLabelKey),
	)

	err = a(ctx, &rr)
	g.Expect(err).NotTo(HaveOccurred())

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cmCustom), &corev1.ConfigMap{})
	g.Expect(err).To(MatchError(k8serr.IsNotFound, "IsNotFound"))

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cmDefault), &corev1.ConfigMap{})
	g.Expect(err).NotTo(HaveOccurred())
}

func TestGcActionOnce(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	cli := sharedEnvTest.Client()
	nsn := xid.New().String()

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).
		NotTo(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		Instance: &componentApi.Dashboard{
			TypeMeta: metav1.TypeMeta{
				APIVersion: componentApi.GroupVersion.String(),
				Kind:       componentApi.DashboardKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DashboardInstanceName,
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{
				Version: semver.Version{Major: 0, Minor: 2, Patch: 0},
			},
		},
		Generated: true,
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("GetClient").Return(sharedEnvTest.Client())
			m.On("GetDynamicClient").Return(sharedEnvTest.DynamicClient())
			m.On("GetDiscoveryClient").Return(sharedEnvTest.DiscoveryClient())
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	g.Expect(cli.Create(ctx, rr.Instance)).
		NotTo(HaveOccurred())

	t.Cleanup(func() {
		g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	})

	cm := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: nsn,
		Name:      xid.New().String(),
		Annotations: map[string]string{
			annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
			annotations.InstanceUID:        xid.New().String(),
			annotations.PlatformType:       string(cluster.OpenDataHub),
			annotations.PlatformVersion:    rr.Release.Version.String(),
		},
		Labels: map[string]string{
			labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
		},
	}}

	g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm, cli.Scheme())).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cm)).
		NotTo(HaveOccurred())

	a := gc.NewAction(gc.WithDeletePropagationPolicy(metav1.DeletePropagationBackground), gc.InNamespace(nsn))

	gc.DeletedTotal.Reset()
	gc.DeletedTotal.WithLabelValues("dashboard").Add(0)

	g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())
	deletedAfterFirstRun := testutil.ToFloat64(gc.DeletedTotal)
	g.Expect(deletedAfterFirstRun).Should(BeNumerically("==", 1))

	g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())
	g.Expect(testutil.ToFloat64(gc.DeletedTotal)).Should(BeNumerically("==", deletedAfterFirstRun))
}
