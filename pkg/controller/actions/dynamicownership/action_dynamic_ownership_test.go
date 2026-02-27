package dynamicownership_test

import (
	"context"
	"io"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dynamicownership"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

//nolint:gochecknoinits
func init() {
	log.SetLogger(zap.New(zap.WriteTo(io.Discard)))
}

func TestDynamicOwnershipAction_SkipsWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := xid.New().String()
	name := xid.New().String()

	obj := createConfigMap(t, g, name, ns)

	watchRegistrarCalled := false
	action := dynamicownership.NewAction(
		func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
			watchRegistrarCalled = true
			return nil
		},
		gvk.Dashboard,
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*obj},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(false)
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(watchRegistrarCalled).To(BeFalse(), "Watch registrar should not be called when dynamic ownership is disabled")
}

func TestDynamicOwnershipAction(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	ns := xid.New().String()
	cmName := xid.New().String()
	secretName := xid.New().String()
	deploymentName := xid.New().String()

	cm := createConfigMap(t, g, cmName, ns)
	secret := createSecret(t, g, secretName, ns)
	deployment := createDeployment(t, g, deploymentName, ns)

	watchRegistrarCalledWithObjects := map[string]bool{}
	action := dynamicownership.NewAction(
		func(obj client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
			watchRegistrarCalledWithObjects[obj.GetObjectKind().GroupVersionKind().String()] = true
			return nil
		},
		gvk.Dashboard,
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*cm, *secret, *deployment},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", gvk.Deployment).Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", gvk.ConfigMap).Return(true)  // Statically owned, already watched
			m.On("Owns", mock.Anything).Return(false) // Any other GVK is not statically owned
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(watchRegistrarCalledWithObjects).To(HaveLen(1), "Watch registrar should be called only for non-statically owned GVK")
	g.Expect(watchRegistrarCalledWithObjects[gvk.ConfigMap.String()]).To(BeFalse(), "Watch registrar should NOT be called for statically owned ConfigMap")
	g.Expect(watchRegistrarCalledWithObjects[gvk.Secret.String()]).To(BeTrue(), "Watch registrar should be called for non-statically owned Secret")
	g.Expect(watchRegistrarCalledWithObjects[gvk.Deployment.String()]).To(BeFalse(), "Watch registrar should be NOT called for excluded Deployment")
}

func TestDynamicOwnershipAction_CRDWatch(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := xid.New().String()
	cmName := xid.New().String()

	crdName := "myresource.test.crd.opendatahub.io"
	crd := createCRD(t, g, crdName)
	cm := createConfigMap(t, g, cmName, ns)

	type watchCall struct {
		gvk        string
		predicates []predicate.Predicate
	}
	var watchCalls []watchCall

	action := dynamicownership.NewAction(
		func(obj client.Object, _ handler.EventHandler, predicates ...predicate.Predicate) error {
			call := watchCall{gvk: obj.GetObjectKind().GroupVersionKind().String()}
			if len(predicates) > 0 {
				call.predicates = predicates
			}
			watchCalls = append(watchCalls, call)
			return nil
		},
		gvk.Dashboard,
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*crd, *cm},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", mock.Anything).Return(false) // Nothing is statically owned
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(watchCalls).To(HaveLen(2))

	var otherCalls []watchCall
	for _, call := range watchCalls {
		if call.gvk != gvk.CustomResourceDefinition.String() {
			otherCalls = append(otherCalls, call)
			continue
		}
		g.Expect(call.predicates).To(HaveLen(1))
		predicate := call.predicates[0]
		g.Expect(predicate.Create(event.TypedCreateEvent[client.Object]{Object: crd})).To(BeTrue())
		g.Expect(predicate.Update(event.TypedUpdateEvent[client.Object]{ObjectOld: crd, ObjectNew: crd})).To(BeTrue())
		g.Expect(predicate.Delete(event.TypedDeleteEvent[client.Object]{Object: crd})).To(BeTrue())
	}
	g.Expect(otherCalls).To(HaveLen(1))
	g.Expect(otherCalls[0].gvk).To(Equal(gvk.ConfigMap.String()))
}

func TestDynamicOwnershipAction_ManagedByFalseWithSameGVK(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := xid.New().String()
	regularCMName := xid.New().String()
	managedFalseCMName := xid.New().String()

	regularCM := createConfigMap(t, g, regularCMName, ns)
	managedFalseCM := createConfigMapWithManagedByFalse(t, g, managedFalseCMName, ns)

	type watchCall struct {
		gvk        string
		predicates []predicate.Predicate
	}
	var watchCalls []watchCall

	action := dynamicownership.NewAction(
		func(obj client.Object, _ handler.EventHandler, predicates ...predicate.Predicate) error {
			call := watchCall{
				gvk:        obj.GetObjectKind().GroupVersionKind().String(),
				predicates: predicates,
			}
			watchCalls = append(watchCalls, call)
			return nil
		},
		gvk.Dashboard,
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*regularCM, *managedFalseCM},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", mock.Anything).Return(false) // Nothing is statically owned
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Should have 2 watch calls - one for regular ConfigMap, one for managed-by-false ConfigMap
	g.Expect(watchCalls).To(HaveLen(2), "Should register watches for both regular and managed-by-false ConfigMaps")

	// Verify both ConfigMap watches were registered
	g.Expect(watchCalls[0].gvk).To(Equal(gvk.ConfigMap.String()))
	g.Expect(watchCalls[1].gvk).To(Equal(gvk.ConfigMap.String()))

	managedFalseWatchPredicate := watchCalls[1].predicates[0]
	// The Deleted predicate should only pass delete events
	g.Expect(managedFalseWatchPredicate.Create(event.TypedCreateEvent[client.Object]{Object: managedFalseCM})).To(BeFalse())
	g.Expect(managedFalseWatchPredicate.Update(event.TypedUpdateEvent[client.Object]{ObjectOld: managedFalseCM, ObjectNew: managedFalseCM})).To(BeFalse())
	g.Expect(managedFalseWatchPredicate.Delete(event.TypedDeleteEvent[client.Object]{Object: managedFalseCM})).To(BeTrue())
}

func TestDynamicOwnershipAction_CRDWatchDeduplication(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	crdName := "myresource.test.crd.opendatahub.io"
	crd1 := createCRD(t, g, crdName)
	crd2 := createCRD(t, g, crdName) // Same CRD name

	watchCallCount := 0
	action := dynamicownership.NewAction(
		func(_ client.Object, _ handler.EventHandler, _ ...predicate.Predicate) error {
			watchCallCount++
			return nil
		},
		gvk.Dashboard,
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*crd1, *crd2},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", mock.Anything).Return(false)
		}),
	}

	// First reconciliation
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(watchCallCount).To(Equal(1), "Should only register one watch for duplicate CRD")

	// Second reconciliation with same CRDs
	watchCallCount = 0
	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(watchCallCount).To(Equal(0), "Should not re-register watch for already watched CRD")
}

func TestDynamicOwnershipAction_WithCustomManagedByFalseMatcher(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := xid.New().String()
	cmName := xid.New().String()
	secretName := xid.New().String()

	cm := createConfigMap(t, g, cmName, ns)
	secret := createSecret(t, g, secretName, ns)

	type watchCall struct {
		gvk        string
		predicates []predicate.Predicate
	}
	var watchCalls []watchCall

	// Custom matcher that marks ConfigMaps as "managed-by-false" and Secrets as normal
	customMatcher := func(res *unstructured.Unstructured) bool {
		return res.GetKind() == "ConfigMap"
	}

	action := dynamicownership.NewAction(
		func(obj client.Object, _ handler.EventHandler, predicates ...predicate.Predicate) error {
			call := watchCall{
				gvk:        obj.GetObjectKind().GroupVersionKind().String(),
				predicates: predicates,
			}
			watchCalls = append(watchCalls, call)
			return nil
		},
		gvk.Dashboard,
		dynamicownership.WithManagedByFalseMatcher(customMatcher),
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*cm, *secret},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", mock.Anything).Return(false) // Nothing is statically owned
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(watchCalls).To(HaveLen(2))

	// ConfigMap should be treated as managed-by-false (custom matcher returns true)
	cmCall := watchCalls[0]
	g.Expect(cmCall.gvk).To(Equal(gvk.ConfigMap.String()))
	g.Expect(cmCall.predicates).To(HaveLen(1))
	// Deleted predicate should only pass delete events
	g.Expect(cmCall.predicates[0].Create(event.TypedCreateEvent[client.Object]{Object: cm})).To(BeFalse())
	g.Expect(cmCall.predicates[0].Delete(event.TypedDeleteEvent[client.Object]{Object: cm})).To(BeTrue())

	// Secret should be treated as normal (custom matcher returns false)
	secretCall := watchCalls[1]
	g.Expect(secretCall.gvk).To(Equal(gvk.Secret.String()))
	g.Expect(secretCall.predicates[0].Create(event.TypedCreateEvent[client.Object]{Object: secret})).To(BeTrue())
	g.Expect(secretCall.predicates[0].Update(event.TypedUpdateEvent[client.Object]{ObjectOld: secret, ObjectNew: secret})).To(BeTrue())
	g.Expect(secretCall.predicates[0].Delete(event.TypedDeleteEvent[client.Object]{Object: secret})).To(BeTrue())
}

func TestDynamicOwnershipAction_WithGVKPredicates(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := xid.New().String()
	cmName := xid.New().String()
	deploymentName := xid.New().String()

	cm := createConfigMap(t, g, cmName, ns)
	deployment := createDeployment(t, g, deploymentName, ns)

	type watchCall struct {
		gvk        string
		predicates []predicate.Predicate
	}
	var watchCalls []watchCall

	// Custom predicate that always returns true for create
	customPredicate := predicate.Funcs{
		CreateFunc: func(_ event.TypedCreateEvent[client.Object]) bool { return true },
		UpdateFunc: func(_ event.TypedUpdateEvent[client.Object]) bool { return false },
		DeleteFunc: func(_ event.TypedDeleteEvent[client.Object]) bool { return false },
	}

	action := dynamicownership.NewAction(
		func(obj client.Object, _ handler.EventHandler, predicates ...predicate.Predicate) error {
			call := watchCall{
				gvk:        obj.GetObjectKind().GroupVersionKind().String(),
				predicates: predicates,
			}
			watchCalls = append(watchCalls, call)
			return nil
		},
		gvk.Dashboard,
		dynamicownership.WithGVKPredicates(map[schema.GroupVersionKind][]predicate.Predicate{
			gvk.Deployment: {customPredicate},
		}),
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dashboard",
			},
		},
		Resources: []unstructured.Unstructured{*cm, *deployment},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("IsDynamicOwnershipEnabled").Return(true)
			m.On("IsExcludedFromOwnership", mock.Anything).Return(false)
			m.On("Owns", mock.Anything).Return(false) // Nothing is statically owned
		}),
	}

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(watchCalls).To(HaveLen(2))

	// Find the Deployment watch call
	var deploymentCall, cmCall watchCall
	for _, call := range watchCalls {
		if call.gvk == gvk.Deployment.String() {
			deploymentCall = call
		} else if call.gvk == gvk.ConfigMap.String() {
			cmCall = call
		}
	}

	// Deployment should use custom predicate
	g.Expect(deploymentCall.predicates).To(HaveLen(1))
	g.Expect(deploymentCall.predicates[0].Create(event.TypedCreateEvent[client.Object]{Object: deployment})).To(BeTrue())
	g.Expect(deploymentCall.predicates[0].Update(event.TypedUpdateEvent[client.Object]{ObjectOld: deployment, ObjectNew: deployment})).To(BeFalse())
	g.Expect(deploymentCall.predicates[0].Delete(event.TypedDeleteEvent[client.Object]{Object: deployment})).To(BeFalse())

	// ConfigMap should use default predicate (not the custom one)
	g.Expect(cmCall.predicates).To(HaveLen(1))
	g.Expect(cmCall.predicates[0].Create(event.TypedCreateEvent[client.Object]{Object: cm})).To(BeTrue())
	g.Expect(cmCall.predicates[0].Update(event.TypedUpdateEvent[client.Object]{ObjectOld: cm, ObjectNew: cm})).To(BeTrue())
	g.Expect(cmCall.predicates[0].Delete(event.TypedDeleteEvent[client.Object]{Object: cm})).To(BeTrue())
}

func createConfigMap(t *testing.T, g Gomega, name, ns string) *unstructured.Unstructured {
	t.Helper()

	obj, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}

func createConfigMapWithManagedByFalse(t *testing.T, g Gomega, name, ns string) *unstructured.Unstructured {
	t.Helper()

	obj, err := resources.ToUnstructured(&corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Annotations: map[string]string{
				annotations.ManagedByODHOperator: "false",
			},
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}

func createSecret(t *testing.T, g Gomega, name, ns string) *unstructured.Unstructured {
	t.Helper()

	obj, err := resources.ToUnstructured(&corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       gvk.Secret.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}

func createDeployment(t *testing.T, g Gomega, name, ns string) *unstructured.Unstructured {
	t.Helper()

	obj, err := resources.ToUnstructured(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       gvk.Deployment.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}

func createCRD(t *testing.T, g Gomega, name string) *unstructured.Unstructured {
	t.Helper()

	obj, err := resources.ToUnstructured(&apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       gvk.CustomResourceDefinition.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	return obj
}
