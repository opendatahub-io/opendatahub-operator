package gc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	gTypes "github.com/onsi/gomega/types"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apytypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	gcSvc "github.com/opendatahub-io/opendatahub-operator/v2/pkg/services/gc"

	. "github.com/onsi/gomega"
)

func TestGcAction(t *testing.T) {
	g := NewWithT(t)

	s := runtime.NewScheme()
	ctx := context.Background()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(authorizationv1.AddToScheme(s))

	envTest := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme:          s,
			CleanUpAfterUse: true,
		},
	}

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := ctrlCli.New(cfg, ctrlCli.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := client.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli).NotTo(BeNil())

	tests := []struct {
		name           string
		version        semver.Version
		generated      bool
		matcher        gTypes.GomegaMatcher
		metricsMatcher gTypes.GomegaMatcher
		labels         map[string]string
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
			nsn := xid.New().String()
			id := xid.New().String()

			gci := gcSvc.New(
				cli,
				nsn,
				// This is required as there are no kubernetes controller running
				// with the envtest, hence we can't use the foreground deletion
				// policy (default)
				gcSvc.WithPropagationPolicy(metav1.DeletePropagationBackground),
			)

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())
			g.Expect(gci.Start(ctx)).
				NotTo(HaveOccurred())

			rr := types.ReconciliationRequest{
				Client: cli,
				DSCI: &dsciv1.DSCInitialization{
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
				},
				Instance: &componentApi.Dashboard{
					TypeMeta: metav1.TypeMeta{
						APIVersion: componentApi.GroupVersion.String(),
						Kind:       componentApi.DashboardKind,
					},
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
						UID:        apytypes.UID(id),
					},
				},
				Release: common.Release{
					Name: cluster.OpenDataHub,
					Version: version.OperatorVersion{
						Version: tt.version,
					},
				},
				Generated: tt.generated,
			}

			l := make(map[string]string)
			for k, v := range tt.labels {
				l[k] = v
			}

			l[labels.PlatformPartOf] = strings.ToLower(componentApi.DashboardKind)

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gc-cm",
					Namespace: nsn,
					Annotations: map[string]string{
						annotations.InstanceGeneration: "1",
						annotations.InstanceUID:        tt.uidFn(&rr),
						annotations.PlatformVersion:    "0.1.0",
						annotations.PlatformType:       string(cluster.OpenDataHub),
					},
					Labels: l,
				},
			}

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			opts := make([]gc.ActionOpts, 0, len(tt.options)+1)
			opts = append(opts, gc.WithGC(gci))
			opts = append(opts, tt.options...)

			a := gc.NewAction(opts...)

			err = a(ctx, &rr)
			g.Expect(err).NotTo(HaveOccurred())

			if tt.matcher != nil {
				err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm), &corev1.ConfigMap{})
				g.Expect(err).To(tt.matcher)
			}

			if tt.metricsMatcher != nil {
				ct := testutil.ToFloat64(gc.CyclesTotal)
				g.Expect(ct).Should(tt.metricsMatcher)
			}
		})
	}
}

func TestGcActionCluster(t *testing.T) {
	g := NewWithT(t)

	s := runtime.NewScheme()
	ctx := context.Background()
	id := xid.New().String()
	nsn := xid.New().String()

	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(authorizationv1.AddToScheme(s))
	utilruntime.Must(rbacv1.AddToScheme(s))

	envTest := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme:          s,
			CleanUpAfterUse: true,
		},
	}

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := ctrlCli.New(cfg, ctrlCli.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := client.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli).NotTo(BeNil())

	gci := gcSvc.New(
		cli,
		nsn,
		// This is required as there are no kubernetes controller running
		// with the envtest, hence we can't use the foreground deletion
		// policy (default)
		gcSvc.WithPropagationPolicy(metav1.DeletePropagationBackground),
	)

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).
		NotTo(HaveOccurred())
	g.Expect(gci.Start(ctx)).
		NotTo(HaveOccurred())

	rr := types.ReconciliationRequest{
		Client: cli,
		DSCI: &dsciv1.DSCInitialization{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
		},
		Instance: &componentApi.Dashboard{
			TypeMeta: metav1.TypeMeta{
				APIVersion: componentApi.GroupVersion.String(),
				Kind:       componentApi.DashboardKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				UID:        apytypes.UID(id),
			},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
			Version: version.OperatorVersion{
				Version: semver.Version{Major: 0, Minor: 2, Patch: 0},
			},
		},
		Generated: true,
	}

	om := metav1.ObjectMeta{
		Namespace: nsn,
		Annotations: map[string]string{
			annotations.InstanceGeneration: "1",
			annotations.InstanceUID:        id,
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

	g.Expect(cli.Create(ctx, &cm1)).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cm2)).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cr1)).
		NotTo(HaveOccurred())

	g.Expect(cli.Create(ctx, &cr2)).
		NotTo(HaveOccurred())

	a := gc.NewAction(gc.WithGC(gci))

	gc.DeletedTotal.Reset()
	gc.DeletedTotal.WithLabelValues("dashboard").Add(0)

	err = a(ctx, &rr)
	g.Expect(err).NotTo(HaveOccurred())

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm1), &corev1.ConfigMap{})
	g.Expect(err).To(MatchError(k8serr.IsNotFound, "IsNotFound"))

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm2), &corev1.ConfigMap{})
	g.Expect(err).ToNot(HaveOccurred())

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cr1), &rbacv1.ClusterRole{})
	g.Expect(err).To(MatchError(k8serr.IsNotFound, "IsNotFound"))

	err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cr2), &rbacv1.ClusterRole{})
	g.Expect(err).ToNot(HaveOccurred())

	ct := testutil.ToFloat64(gc.DeletedTotal)
	g.Expect(ct).Should(BeNumerically("==", 2))
}
