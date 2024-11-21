package gc_test

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	gTypes "github.com/onsi/gomega/types"
	"github.com/operator-framework/api/pkg/lib/version"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
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
		hashFn         func(*types.ReconciliationRequest) (string, error)
	}{
		{
			name:           "should delete leftovers",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Satisfy(k8serr.IsNotFound),
			metricsMatcher: BeNumerically("==", 1),
		},
		{
			name:           "should not delete resources because same annotations",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
		},
		{
			name:           "should not delete resources because of no generated resources have been detected",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      false,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 0),
		},
		{
			name:           "should not delete resources because of selector",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			labels:         map[string]string{"foo": "bar"},
			options:        []gc.ActionOpts{gc.WithLabel("foo", "baz")},
		},
		{
			name:           "should not delete resources because of unremovable type",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			options:        []gc.ActionOpts{gc.WithUnremovables(gvk.ConfigMap)},
		},
		{
			name:           "should not delete resources because of predicate",
			version:        semver.Version{Major: 0, Minor: 0, Patch: 1},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			options: []gc.ActionOpts{gc.WithPredicate(
				func(request *types.ReconciliationRequest, unstructured unstructured.Unstructured) (bool, error) {
					return unstructured.GroupVersionKind() != gvk.ConfigMap, nil
				},
			)},
		},
		{
			name:           "should delete leftovers because of hash",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Satisfy(k8serr.IsNotFound),
			metricsMatcher: BeNumerically("==", 1),
			hashFn: func(rr *types.ReconciliationRequest) (string, error) {
				return xid.New().String(), nil
			},
		},
		{
			name:           "should not delete leftovers because of hash",
			version:        semver.Version{Major: 0, Minor: 1, Patch: 0},
			generated:      true,
			matcher:        Not(HaveOccurred()),
			metricsMatcher: BeNumerically("==", 1),
			hashFn:         types.HashStr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gc.CyclesTotal.Reset()
			gc.CyclesTotal.WithLabelValues("dashboard").Add(0)

			g := NewWithT(t)
			nsn := xid.New().String()

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
				OwnerName: componentsv1.DashboardInstanceName,
				DSCI: &dsciv1.DSCInitialization{
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
				},
				Instance: &componentsv1.Dashboard{
					TypeMeta: metav1.TypeMeta{
						APIVersion: componentsv1.GroupVersion.String(),
						Kind:       componentsv1.DashboardKind,
					},
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
				},
				Release: cluster.Release{
					Name: cluster.OpenDataHub,
					Version: version.OperatorVersion{
						Version: tt.version,
					},
				},
				Generated: tt.generated,
			}

			ch := ""
			if tt.hashFn != nil {
				ch, err = tt.hashFn(&rr)
				g.Expect(err).NotTo(HaveOccurred())
			}

			l := make(map[string]string)
			for k, v := range tt.labels {
				l[k] = v
			}

			l[labels.ComponentPartOf] = rr.OwnerName

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gc-cm",
					Namespace: nsn,
					Annotations: map[string]string{
						annotations.ComponentGeneration: "1",
						annotations.ComponentHash:       ch,
						annotations.PlatformVersion:     "0.1.0",
						annotations.PlatformType:        string(cluster.OpenDataHub),
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
