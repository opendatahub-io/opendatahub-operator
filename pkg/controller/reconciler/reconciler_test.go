//nolint:testpackage
package reconciler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gomegaTypes "github.com/onsi/gomega/types"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func createEnvTest(s *runtime.Scheme) (*envtest.Environment, error) {
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))

	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		return nil, err
	}

	envTest := envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	return &envTest, nil
}

func createReconciler(cli *odhClient.Client) *Reconciler {
	return &Reconciler{
		Client:   cli,
		Scheme:   cli.Scheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("test"),
		Release:  cluster.GetRelease(),
		Recorder: record.NewFakeRecorder(100),
		name:     "test",
		instanceFactory: func() (common.PlatformObject, error) {
			i := &componentApi.Dashboard{
				TypeMeta: ctrl.TypeMeta{
					APIVersion: gvk.Dashboard.GroupVersion().String(),
					Kind:       gvk.Dashboard.Kind,
				},
			}

			return i, nil
		},
		conditionsManagerFactory: func(accessor common.ConditionsAccessor) *conditions.Manager {
			return conditions.NewManager(accessor, status.ConditionTypeReady)
		},
	}
}

func TestConditions(t *testing.T) {
	ctx := context.Background()

	g := NewWithT(t)
	s := runtime.NewScheme()

	envTest, err := createEnvTest(s)
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := odhClient.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())

	dsci := resources.GvkToUnstructured(gvk.DSCInitialization)
	dsci.SetName(xid.New().String())
	dsci.SetGeneration(1)

	err = cli.Create(ctx, dsci)
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name    string
		err     error
		matcher gomegaTypes.GomegaMatcher
	}{
		{
			name: "ready",
			err:  nil,
			matcher: And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
			),
		},
		{
			name: "stop",
			err:  odherrors.NewStopError("stop"),
			matcher: And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
		{
			name: "failure",
			err:  errors.New("failure"),
			matcher: And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionFalse),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dash := resources.GvkToUnstructured(gvk.Dashboard)
			dash.SetName(componentApi.DashboardInstanceName)
			dash.SetGeneration(1)

			err = cli.Create(ctx, dash)
			g.Expect(err).NotTo(HaveOccurred())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: componentApi.DashboardInstanceName,
				},
			}

			cc := createReconciler(cli)
			cc.AddAction(func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
				return tt.err
			})

			result, err := cc.Reconcile(ctx, req)
			if tt.err == nil {
				g.Expect(err).ShouldNot(HaveOccurred())
			} else {
				g.Expect(err).Should(MatchError(tt.err))
			}

			g.Expect(result.Requeue).Should(BeFalse())

			di := dash.DeepCopy()
			err = cli.Get(ctx, client.ObjectKeyFromObject(di), di)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(di).Should(tt.matcher)

			err = cli.Delete(ctx, di, client.PropagationPolicy(metav1.DeletePropagationBackground))
			g.Expect(err).ShouldNot(HaveOccurred())

			g.Eventually(func() ([]componentApi.Dashboard, error) {
				l := componentApi.DashboardList{}
				if err := cli.List(ctx, &l, client.InNamespace("")); err != nil {
					return nil, err
				}

				return l.Items, nil
			}).WithTimeout(10 * time.Second).Should(BeEmpty())
		})
	}
}
