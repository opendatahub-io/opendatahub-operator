//nolint:testpackage // Verifies the mutation boundary in the shared reconciler.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	reconcilertest "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestReconcile_BeforeReconciliationStopsBeforeWrites(t *testing.T) {
	checkErr := errors.New("cannot verify readiness")
	for _, deleting := range []bool{false, true} {
		for _, failedRead := range []bool{false, true} {
			name := "normal"
			if deleting {
				name = "deleting"
			}
			if failedRead {
				name += "/error"
			} else {
				name += "/failure"
			}
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
				if deleting {
					now := metav1.Now()
					obj.DeletionTimestamp = &now
					obj.Finalizers = []string{platformFinalizer}
				}
				updates, statusApplies := 0, 0
				cli, err := fakeclient.New(fakeclient.WithObjects(obj), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
						updates++
						return nil
					},
					SubResourceApply: func(context.Context, client.Client, string, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
						statusApplies++
						return nil
					},
				}))
				g.Expect(err).NotTo(HaveOccurred())
				r := createReconciler(cli)
				r.ManifestsBasePath, r.ChartsBasePath = "manifests", "charts"
				ordinaryCalls, actionCalls, finalizerCalls, conditionCalls, hookCalls, earlyCalls := 0, 0, 0, 0, 0, 0
				r.conditionsManagerFactory = func(common.ConditionsAccessor) *conditions.Manager {
					conditionCalls++
					return nil
				}
				r.Actions = []actions.Fn{func(context.Context, *types.ReconciliationRequest) error {
					actionCalls++
					return nil
				}}
				r.Finalizer = []actions.Fn{func(context.Context, *types.ReconciliationRequest) error {
					finalizerCalls++
					return nil
				}}
				r.postStatusFns = []PostStatusFn{func(context.Context, *types.ReconciliationRequest, bool) error {
					hookCalls++
					return nil
				}}
				r.preConditions = []precondition.PreCondition{
					precondition.Custom(func(context.Context, *types.ReconciliationRequest) (precondition.CheckResult, error) {
						ordinaryCalls++
						return precondition.CheckResult{Pass: true}, nil
					}),
					precondition.Custom(func(_ context.Context, rr *types.ReconciliationRequest) (precondition.CheckResult, error) {
						earlyCalls++
						g.Expect(rr.Conditions).To(BeNil())
						g.Expect(rr.Client.Scheme()).To(BeIdenticalTo(cli.Scheme()))
						g.Expect(rr.Controller).To(BeIdenticalTo(r))
						g.Expect(rr.Instance.GetName()).To(Equal(obj.Name))
						g.Expect(rr.Instance.GetObjectKind().GroupVersionKind()).To(Equal(scheme.TestPlatformObjectGVK))
						g.Expect(rr.Release).To(Equal(r.Release))
						g.Expect(rr.ManifestsBasePath).To(Equal(r.ManifestsBasePath))
						g.Expect(rr.ChartsBasePath).To(Equal(r.ChartsBasePath))
						if failedRead {
							return precondition.CheckResult{}, checkErr
						}
						return precondition.CheckResult{Message: "not ready"}, nil
					}, precondition.BeforeReconciliation()),
				}
				result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
				if failedRead {
					g.Expect(errors.Is(err, checkErr)).To(BeTrue())
					g.Expect(result).To(Equal(ctrl.Result{}))
				} else {
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(result.RequeueAfter).To(Equal(5 * time.Second))
				}
				g.Expect(earlyCalls).To(Equal(1))
				g.Expect([]int{updates, statusApplies, ordinaryCalls, actionCalls, finalizerCalls, conditionCalls, hookCalls}).To(Equal([]int{0, 0, 0, 0, 0, 0, 0}))
				recorder, ok := r.Recorder.(*events.FakeRecorder)
				g.Expect(ok).To(BeTrue())
				g.Expect(recorder.Events).To(BeEmpty())
			})
		}
	}
}

func TestReconcile_BeforeReconciliationPassesToOrdinaryChecks(t *testing.T) {
	g := NewWithT(t)
	obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	statusApplies := 0
	cli, err := fakeclient.New(fakeclient.WithObjects(obj), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		SubResourceApply: func(context.Context, client.Client, string, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
			statusApplies++
			return nil
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	r := createReconciler(cli)
	var calls []string
	var instance common.PlatformObject
	r.preConditions = []precondition.PreCondition{
		precondition.Custom(func(context.Context, *types.ReconciliationRequest) (precondition.CheckResult, error) {
			calls = append(calls, "ordinary")
			return precondition.CheckResult{Message: "ordinary failure"}, nil
		}, precondition.WithConditionType("Ordinary"), precondition.WithStopReconciliation()),
		precondition.Custom(func(_ context.Context, rr *types.ReconciliationRequest) (precondition.CheckResult, error) {
			calls = append(calls, "before")
			instance = rr.Instance
			g.Expect(rr.Conditions).To(BeNil())
			return precondition.CheckResult{Pass: true}, nil
		}, precondition.BeforeReconciliation(), precondition.WithConditionType("Early")),
	}
	r.Actions = []actions.Fn{func(context.Context, *types.ReconciliationRequest) error {
		t.Fatal("ordinary failure must stop actions")
		return nil
	}}
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RequeueAfter).To(Equal(30*time.Second), "ordinary retries retain their existing duration")
	g.Expect(calls).To(Equal([]string{"before", "ordinary"}))
	g.Expect(statusApplies).To(Equal(1))
	g.Expect(conditions.FindStatusCondition(instance.GetStatus(), "Early")).To(BeNil())
	ordinary := conditions.FindStatusCondition(instance.GetStatus(), "Ordinary")
	g.Expect(ordinary).NotTo(BeNil())
	g.Expect(ordinary.Message).To(Equal("ordinary failure"))
}

func TestReconcile_BeforeReconciliationAllowsDeletion(t *testing.T) {
	g := NewWithT(t)
	now := metav1.Now()
	obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{
		Name: "test", DeletionTimestamp: &now, Finalizers: []string{platformFinalizer},
	}}
	cli, err := fakeclient.New(fakeclient.WithObjects(obj))
	g.Expect(err).NotTo(HaveOccurred())
	r := createReconciler(cli)
	var calls []string
	r.preConditions = []precondition.PreCondition{
		precondition.Custom(func(context.Context, *types.ReconciliationRequest) (precondition.CheckResult, error) {
			t.Fatal("ordinary checks must not run during deletion")
			return precondition.CheckResult{}, nil
		}),
		precondition.Custom(func(context.Context, *types.ReconciliationRequest) (precondition.CheckResult, error) {
			calls = append(calls, "before")
			return precondition.CheckResult{Pass: true}, nil
		}, precondition.BeforeReconciliation()),
	}
	r.Finalizer = []actions.Fn{func(context.Context, *types.ReconciliationRequest) error {
		calls = append(calls, "finalize")
		return nil
	}}
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(calls).To(Equal([]string{"before", "finalize"}))
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(obj), &scheme.TestPlatformObject{})
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "removing the last finalizer should complete deletion")
}

const conversionTestCRDName = "datascienceclusters.datasciencecluster.opendatahub.io"

type conversionTestReader struct {
	client.Reader

	crd   *apiextensionsv1.CustomResourceDefinition
	err   error
	reads int
}

func (r *conversionTestReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return r.Reader.Get(ctx, key, obj, opts...)
	}
	r.reads++
	if r.err != nil {
		return r.err
	}
	if r.crd == nil || key.Name != r.crd.Name {
		return fmt.Errorf("unexpected conversion CRD request: %s", key.Name)
	}
	r.crd.DeepCopyInto(crd)
	return nil
}

func TestReconcile_CRDConversionUsesLiveReadsAndRecovers(t *testing.T) {
	g := NewWithT(t)
	obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	cachedCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: conversionTestCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionsv1.CustomResourceConversion{Strategy: apiextensionsv1.WebhookConverter},
		},
	}
	writes := &reconcilertest.MutationRecorder{}
	cachedCRDReads := 0
	interceptors := writes.Interceptors()
	interceptors.Get = func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, into client.Object, opts ...client.GetOption) error {
		if _, isCRD := into.(*apiextensionsv1.CustomResourceDefinition); isCRD {
			cachedCRDReads++
		}
		return cli.Get(ctx, key, into, opts...)
	}
	baseClient, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	cli := clientfake.NewClientBuilder().WithScheme(baseClient.Scheme()).WithRESTMapper(baseClient.RESTMapper()).
		WithObjects(obj, cachedCRD).WithStatusSubresource(obj).WithInterceptorFuncs(interceptors).Build()
	liveCRD := cachedCRD.DeepCopy()
	liveCRD.Spec.Conversion.Strategy = apiextensionsv1.NoneConverter
	reader := &conversionTestReader{crd: liveCRD}
	r := createReconciler(cli)
	r.APIReader = reader
	r.preConditions = []precondition.PreCondition{precondition.CRDConversion(conversionTestCRDName, precondition.RequireWebhookAlways)}
	actionCalls := 0
	r.Actions = []actions.Fn{func(ctx context.Context, rr *types.ReconciliationRequest) error {
		actionCalls++
		rr.Instance.SetAnnotations(map[string]string{"reconciled": "true"})
		return rr.Client.Update(ctx, rr.Instance)
	}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)}
	result, err := r.Reconcile(t.Context(), req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RequeueAfter).To(Equal(5 * time.Second))
	g.Expect(writes.Calls()).To(BeEmpty(), "cached Webhook must not overrule live None")
	g.Expect(actionCalls).To(BeZero())

	// Make the cache say None while the API server has restored Webhook.
	cached := &apiextensionsv1.CustomResourceDefinition{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(cachedCRD), cached)).To(Succeed())
	cached.Spec.Conversion.Strategy = apiextensionsv1.NoneConverter
	g.Expect(cli.Update(t.Context(), cached)).To(Succeed())
	cachedCRDReads = 0
	writes.Reset()
	reader.crd.Spec.Conversion.Strategy = apiextensionsv1.WebhookConverter
	result, err = r.Reconcile(t.Context(), req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RequeueAfter).To(BeZero())
	g.Expect(writes.Calls()).To(ContainElements("update", "status/apply"))
	g.Expect(actionCalls).To(Equal(1))
	saved := &scheme.TestPlatformObject{}
	g.Expect(cli.Get(t.Context(), req.NamespacedName, saved)).To(Succeed())
	g.Expect(saved.Annotations).To(HaveKeyWithValue("reconciled", "true"))
	g.Expect(saved.Status.Conditions).NotTo(BeEmpty(), "the passing path must persist real status")

	// Passing once must not memoize the result for later reconciles.
	reader.crd.Spec.Conversion = nil
	writes.Reset()
	result, err = r.Reconcile(t.Context(), req)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RequeueAfter).To(Equal(5 * time.Second))
	g.Expect(writes.Calls()).To(BeEmpty())
	g.Expect(actionCalls).To(Equal(1))
	g.Expect(reader.reads).To(Equal(3))
	g.Expect(cachedCRDReads).To(BeZero())
}

func TestReconcile_CRDConversionReadErrorsDoNotWrite(t *testing.T) {
	crdResource := schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"}
	readErrors := []error{
		k8serr.NewNotFound(crdResource, conversionTestCRDName),
		k8serr.NewForbidden(crdResource, conversionTestCRDName, errors.New("denied")),
		k8serr.NewInternalError(errors.New("API unavailable")),
		k8serr.NewTimeoutError("API timeout", 1),
		context.Canceled,
	}
	for _, readErr := range readErrors {
		t.Run(readErr.Error(), func(t *testing.T) {
			g := NewWithT(t)
			writes := &reconcilertest.MutationRecorder{}
			obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
			cli, err := fakeclient.New(fakeclient.WithObjects(obj), fakeclient.WithInterceptorFuncs(writes.Interceptors()))
			g.Expect(err).NotTo(HaveOccurred())
			r := createReconciler(cli)
			r.APIReader = &conversionTestReader{err: readErr}
			r.preConditions = []precondition.PreCondition{precondition.CRDConversion(conversionTestCRDName, precondition.RequireWebhookAlways)}
			_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
			g.Expect(errors.Is(err, readErr)).To(BeTrue())
			g.Expect(writes.Calls()).To(BeEmpty())
		})
	}
}

func TestReconcile_CRDConversionMissingReaderOrPrimary(t *testing.T) {
	g := NewWithT(t)
	writes := &reconcilertest.MutationRecorder{}
	obj := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	cli, err := fakeclient.New(fakeclient.WithObjects(obj), fakeclient.WithInterceptorFuncs(writes.Interceptors()))
	g.Expect(err).NotTo(HaveOccurred())
	r := createReconciler(cli)
	r.preConditions = []precondition.PreCondition{precondition.CRDConversion(conversionTestCRDName, precondition.RequireWebhookAlways)}
	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	g.Expect(err).To(HaveOccurred())
	g.Expect(writes.Calls()).To(BeEmpty())

	reader := &conversionTestReader{err: errors.New("must not read when primary is missing")}
	r.APIReader = reader
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "absent"}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(reader.reads).To(BeZero())
	g.Expect(writes.Calls()).To(BeEmpty())
}
