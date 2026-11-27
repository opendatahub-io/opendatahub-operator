package modules

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blang/semver/v4"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const legacyMigrationRetry = 30 * time.Second

// TODO: Remove this migration once upgrades from 3.6 are no longer supported.
type legacyMigration struct {
	scan func(context.Context, *odhtype.ReconciliationRequest) ([]unstructured.Unstructured, error)

	// Platform is a singleton. The work queue serializes reconciles for this key.
	uid      k8stypes.UID
	complete bool
}

func newLegacyMigration() *legacyMigration {
	return &legacyMigration{scan: listLegacyModuleResources}
}

func (m *legacyMigration) run(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	platform, ok := rr.Instance.(*configApi.Platform)
	if !ok {
		return fmt.Errorf("legacy migration requires Platform instance, got %T", rr.Instance)
	}
	if m.uid != platform.UID {
		m.uid = platform.UID
		m.complete = false
	}
	if m.complete {
		return nil
	}
	if err := m.deleteObsoleteResources(ctx, rr, platform); err != nil {
		return retryLegacyMigration(ctx, err)
	}
	m.complete = true
	return nil
}

func retryLegacyMigration(ctx context.Context, err error) error {
	logf.FromContext(ctx).V(1).Info("Legacy module cleanup pending", "reason", err.Error())
	return odherrors.NewRequeueAfterError(legacyMigrationRetry)
}

func (m *legacyMigration) deleteObsoleteResources(ctx context.Context, rr *odhtype.ReconciliationRequest, platform *configApi.Platform) error {
	if rr.SkipDeploy || len(ModuleGatePreflightErrors(rr)) != 0 {
		return errors.New("waiting for module upgrade gate preflight before legacy cleanup")
	}
	if err := checkLegacyMigrationReadiness(ctx, rr.Client, platform, rr.Release); err != nil {
		return err
	}

	objects, err := m.scan(ctx, rr)
	if err != nil {
		return fmt.Errorf("scanning for 3.6 module operator resources: %w", err)
	}
	if err := checkMigrationPlatform(ctx, rr.Client, platform); err != nil {
		return err
	}

	var pending int
	for i := range objects {
		obj := &objects[i]
		kind := obj.GroupVersionKind().GroupKind()
		key := client.ObjectKeyFromObject(obj)
		if obj.GetDeletionTimestamp().IsZero() {
			uid, rv := obj.GetUID(), obj.GetResourceVersion()
			if uid == "" || rv == "" {
				return fmt.Errorf("missing deletion preconditions for legacy %s %s", kind, key)
			}
			if err := rr.Client.Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &rv},
				client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				if k8serr.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("deleting legacy %s %s: %w", kind, key, err)
			}
		}
		pending++
	}
	if pending > 0 {
		return fmt.Errorf("waiting for %d legacy module operator resources to disappear", pending)
	}
	return nil
}

func checkMigrationPlatform(ctx context.Context, reader client.Reader, platform *configApi.Platform) error {
	current := &configApi.Platform{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(platform), current); err != nil {
		return fmt.Errorf("checking current Platform for legacy migration: %w", err)
	}
	if current.UID != platform.UID || current.Generation != platform.Generation || !current.DeletionTimestamp.IsZero() {
		return errors.New("the Platform changed during legacy migration")
	}
	return nil
}

func isLegacyModuleResource(platform *configApi.Platform, release common.Release, obj *unstructured.Unstructured) bool {
	switch obj.GroupVersionKind().GroupKind() {
	case gvk.CustomResourceDefinition.GroupKind(), gvk.Namespace.GroupKind(), gvk.Lease.GroupKind():
		return false
	}
	metadata := obj.GetAnnotations()
	if !PlatformOwnsResource(platform, obj) || metadata[annotations.ManagedByODHOperator] == "false" ||
		metadata[annotations.InstanceUID] != string(platform.UID) || metadata[annotations.InstanceName] != platform.Name ||
		metadata[annotations.PlatformType] != string(release.Name) {
		return false
	}
	version, err := semver.Parse(metadata[annotations.PlatformVersion])
	return err == nil && version.Major == 3 && version.Minor == 6
}
