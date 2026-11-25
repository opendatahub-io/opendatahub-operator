//nolint:testpackage // Exercises the Platform migration action with a legacy installation.
package modules

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/blang/semver/v4"
	operatorversion "github.com/operator-framework/api/pkg/lib/version"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

type legacyTestHandler struct {
	provisioningModuleStub

	state CRState
}

func (h *legacyTestHandler) GetModuleCRState(context.Context, client.Client) (CRState, error) {
	return h.state, nil
}

func TestLegacyMigrationWaitsAndDoesNotRepeatDeletes(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	withTestRegistry(t)
	handler := &legacyTestHandler{provisioningModuleStub: provisioningModuleStub{moduleName: "removed"}, state: CRStateDeleting}
	DefaultRegistry().Add(handler)

	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{
		Name: configApi.PlatformInstanceName, UID: "legacy-platform", Generation: 1,
		Annotations: map[string]string{"example.org/keep": "true"},
	}}
	release := common.Release{Name: cluster.XKS, Version: operatorversion.OperatorVersion{Version: semver.MustParse("3.7.0")}}
	old := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "absent-from-new-manifests", Namespace: "apps", UID: "legacy-object",
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
		Finalizers:      []string{"test.io/hold"},
		Annotations: map[string]string{
			annotations.InstanceUID:     string(platform.UID),
			annotations.InstanceName:    platform.Name,
			annotations.PlatformVersion: "3.6.0",
			annotations.PlatformType:    string(release.Name),
		},
	}}
	key := client.ObjectKeyFromObject(old)
	foreign := old.DeepCopy()
	foreign.Name, foreign.UID = "foreign", "foreign-object"
	foreign.OwnerReferences[0].UID = "another-platform"
	unmanaged := old.DeepCopy()
	unmanaged.Name, unmanaged.UID = "unmanaged", "unmanaged-object"
	unmanaged.Annotations[annotations.ManagedByODHOperator] = "false"
	var scans, deletes int
	var scanFailure, deleteFailure bool
	faults := interceptor.Funcs{
		Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deletes++
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			g.Expect(options.Preconditions).NotTo(BeNil())
			g.Expect(options.Preconditions.UID).To(HaveValue(Equal(old.UID)))
			g.Expect(options.Preconditions.ResourceVersion).To(HaveValue(Not(BeEmpty())))
			if deleteFailure {
				return k8serr.NewServiceUnavailable("delete failed")
			}
			return cli.Delete(ctx, obj, opts...)
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, old, foreign, unmanaged), fakeclient.WithInterceptorFuncs(faults))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(platform), platform)).To(Succeed())
	scan := func(ctx context.Context, _ *types.ReconciliationRequest) ([]unstructured.Unstructured, error) {
		scans++
		if scanFailure {
			return nil, k8serr.NewServiceUnavailable("scan incomplete")
		}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.ConfigMap.GroupVersion().WithKind("ConfigMapList"))
		err := cli.List(ctx, list, client.InNamespace(old.Namespace))
		return slices.DeleteFunc(list.Items, func(obj unstructured.Unstructured) bool {
			return !isLegacyModuleResource(platform, release, &obj)
		}), err
	}
	newAction := func() *legacyMigration {
		return &legacyMigration{scan: scan}
	}
	migration := newAction()
	rr := &types.ReconciliationRequest{Client: cli, Instance: platform, Release: release}
	assertPending := func() {
		t.Helper()
		var pending odherrors.RequeueAfterError
		g.Expect(errors.As(migration.run(ctx, rr), &pending)).To(BeTrue())
		g.Expect(pending.After).To(Equal(legacyMigrationRetry))
	}

	assertPending()
	g.Expect(scans).To(BeZero())
	g.Expect(deletes).To(BeZero())
	handler.state = CRStateAbsent
	scanFailure = true
	assertPending()
	g.Expect(scans).To(Equal(1))
	g.Expect(deletes).To(BeZero())

	scanFailure, deleteFailure = false, true
	assertPending()
	g.Expect(deletes).To(Equal(1))
	g.Expect(scans).To(Equal(2))

	deleteFailure = false
	assertPending()
	g.Expect(deletes).To(Equal(2))
	g.Expect(cli.Get(ctx, key, old)).To(Succeed())
	g.Expect(old.DeletionTimestamp.IsZero()).To(BeFalse())

	migration = newAction()
	assertPending()
	g.Expect(deletes).To(Equal(2), "a restart must not repeat a delete already accepted")
	assertPending()
	g.Expect(deletes).To(Equal(2), "a new reconcile must not repeat a delete already accepted")

	old.Finalizers = nil
	g.Expect(cli.Update(ctx, old)).To(Succeed())
	g.Expect(migration.run(ctx, rr)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(foreign), foreign)).To(Succeed())
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(unmanaged), unmanaged)).To(Succeed())
	persisted := &configApi.Platform{}
	g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(platform), persisted)).To(Succeed())
	g.Expect(persisted.GetAnnotations()).To(Equal(map[string]string{"example.org/keep": "true"}))
	rr.Instance = persisted
	completedScans := scans
	g.Expect(migration.run(ctx, rr)).To(Succeed())
	g.Expect(scans).To(Equal(completedScans))

	// A restart checks once for legacy resources, even after cleanup completed.
	migration = newAction()
	g.Expect(migration.run(ctx, rr)).To(Succeed())
	g.Expect(migration.run(ctx, rr)).To(Succeed())
	g.Expect(scans).To(Equal(completedScans + 1))
}
