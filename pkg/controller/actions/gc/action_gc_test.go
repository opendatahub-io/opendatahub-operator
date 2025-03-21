package gc_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	gTypes "github.com/onsi/gomega/types"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc/engine"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
}

func TestGcAction(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()

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
			nsn := xid.New().String()

			gci := engine.New(
				// This is required as there are no kubernetes controller running
				// with the envtest, hence we can't use the foreground deletion
				// policy (default)
				engine.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
			)

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())
			g.Expect(gci.Refresh(ctx, cli, nsn)).
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
			}

			g.Expect(cli.Create(ctx, rr.Instance)).
				NotTo(HaveOccurred())

			defer func() {
				g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			}()

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gc-cm",
					Namespace: nsn,
					Annotations: map[string]string{
						annotations.InstanceGeneration: strconv.FormatInt(rr.Instance.GetGeneration(), 10),
						annotations.InstanceUID:        tt.uidFn(&rr),
						annotations.PlatformVersion:    "0.1.0",
						annotations.PlatformType:       string(cluster.OpenDataHub),
					},
					Labels: map[string]string{
						labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
					},
				},
			}

			for k, v := range tt.labels {
				cm.Labels[k] = v
			}
			for k, v := range tt.annotations {
				cm.Annotations[k] = v
			}

			defer func() {
				g.Expect(cli.Delete(ctx, &cm)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			}()

			g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm, cli.Scheme())).
				NotTo(HaveOccurred())

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			opts := make([]gc.ActionOpts, 0, len(tt.options)+1)
			opts = append(opts, gc.WithEngine(gci))
			opts = append(opts, gc.InNamespace(nsn))
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

func TestGcActionOwn(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()

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

			gci := engine.New(
				// This is required as there are no kubernetes controller running
				// with the envtest, hence we can't use the foreground deletion
				// policy (default)
				engine.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
			)

			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsn,
				},
			}

			g.Expect(cli.Create(ctx, &ns)).
				NotTo(HaveOccurred())
			g.Expect(gci.Refresh(ctx, cli, nsn)).
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
			}

			g.Expect(cli.Create(ctx, rr.Instance)).
				NotTo(HaveOccurred())

			defer func() {
				g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			}()

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

			defer func() {
				g.Expect(cli.Delete(ctx, &cm)).Should(Or(
					Not(HaveOccurred()),
					MatchError(k8serr.IsNotFound, "IsNotFound"),
				))
			}()

			if tt.owned {
				g.Expect(controllerutil.SetOwnerReference(rr.Instance, &cm, cli.Scheme())).
					NotTo(HaveOccurred())
			}

			g.Expect(cli.Create(ctx, &cm)).
				NotTo(HaveOccurred())

			opts := make([]gc.ActionOpts, 0, len(tt.options)+1)
			opts = append(opts, gc.WithEngine(gci))
			opts = append(opts, gc.InNamespace(nsn))
			opts = append(opts, tt.options...)

			a := gc.NewAction(opts...)

			err = a(ctx, &rr)
			g.Expect(err).NotTo(HaveOccurred())

			if tt.matcher != nil {
				err = cli.Get(ctx, ctrlCli.ObjectKeyFromObject(&cm), &corev1.ConfigMap{})
				g.Expect(err).To(tt.matcher)
			}
		})
	}
}

func TestGcActionCluster(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()
	nsn := xid.New().String()

	gci := engine.New(
		// This is required as there are no kubernetes controller running
		// with the envtest, hence we can't use the foreground deletion
		// policy (default)
		engine.WithDeletePropagationPolicy(metav1.DeletePropagationBackground),
	)

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).
		NotTo(HaveOccurred())
	g.Expect(gci.Refresh(ctx, cli, nsn)).
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
	}

	g.Expect(cli.Create(ctx, rr.Instance)).
		NotTo(HaveOccurred())

	defer func() {
		g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	}()

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

	a := gc.NewAction(gc.WithEngine(gci), gc.InNamespace(nsn))

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

func TestGcActionOnce(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	ctx := context.Background()
	cli := envTest.Client()
	nsn := xid.New().String()

	gci := engine.New(
		// Since test env does not support foreground deletion, we can
		// use it to simulate a resource deleted, but not removed.
		engine.WithDeletePropagationPolicy(metav1.DeletePropagationForeground),
	)

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsn,
		},
	}

	g.Expect(cli.Create(ctx, &ns)).
		NotTo(HaveOccurred())
	g.Expect(gci.Refresh(ctx, cli, nsn)).
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
	}

	g.Expect(cli.Create(ctx, rr.Instance)).
		NotTo(HaveOccurred())

	defer func() {
		g.Expect(cli.Delete(ctx, rr.Instance)).Should(Or(
			Not(HaveOccurred()),
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		))
	}()

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

	a := gc.NewAction(gc.WithEngine(gci), gc.InNamespace(nsn))

	gc.DeletedTotal.Reset()
	gc.DeletedTotal.WithLabelValues("dashboard").Add(0)

	g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())
	g.Expect(testutil.ToFloat64(gc.DeletedTotal)).Should(BeNumerically("==", 1))

	g.Expect(a(ctx, &rr)).NotTo(HaveOccurred())
	g.Expect(testutil.ToFloat64(gc.DeletedTotal)).Should(BeNumerically("==", 1))
}
