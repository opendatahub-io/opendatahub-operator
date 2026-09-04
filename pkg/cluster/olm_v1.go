package cluster

import (
	"context"
	"fmt"

	"github.com/blang/semver/v4"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// OLMv1 helpers for platform detection and uninstall (RHOAIENG-70946).
//
// Runtime detection is still required when ODH_PLATFORM_TYPE is unset: RHOAI bundle
// manager manifests leave it commented out, and managed add-on clusters rely on
// CatalogSource / ClusterCatalog presence instead.

// managedAddonCatalogExists reports whether the managed RHOAI add-on catalog is present (OLMv1).
func managedAddonCatalogExists(ctx context.Context, cli client.Client) (bool, error) {
	catalog := &olmv1.ClusterCatalog{}
	err := cli.Get(ctx, client.ObjectKey{Name: managedAddonCatalogName}, catalog)
	if err != nil {
		if meta.IsNoMatchError(err) || k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// clusterExtensionForPackage returns the ClusterExtension installing the given OLM package, if any.
// The ClusterExtension resource name is arbitrary; matching uses spec.source.catalog.packageName.
// When installNamespace is non-empty, the extension must also target that namespace (spec.namespace).
func clusterExtensionForPackage(ctx context.Context, cli client.Client, packageName, installNamespace string) (*olmv1.ClusterExtension, error) {
	list := &olmv1.ClusterExtensionList{}
	if err := cli.List(ctx, list); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	for i := range list.Items {
		ext := &list.Items[i]
		if installNamespace != "" && ext.Spec.Namespace != installNamespace {
			continue
		}
		if ext.Spec.Source.SourceType != olmv1.SourceTypeCatalog {
			continue
		}
		if ext.Spec.Source.Catalog == nil {
			continue
		}
		if ext.Spec.Source.Catalog.PackageName == packageName {
			return ext, nil
		}
	}
	return nil, nil
}

// getInstalledVersionFromClusterExtension reads the semver of the bundle reported in extension status.
func getInstalledVersionFromClusterExtension(ext *olmv1.ClusterExtension) (semver.Version, error) {
	if ext == nil || ext.Status.Install == nil {
		return semver.Version{}, nil
	}
	versionStr := ext.Status.Install.Bundle.Version
	if versionStr == "" {
		return semver.Version{}, nil
	}
	return semver.ParseTolerant(versionStr)
}

// OperatorOLMPackageName returns the OLM package name for platform detection and uninstall.
func OperatorOLMPackageName(platform common.Platform) string {
	switch platform {
	case SelfManagedRhoai, ManagedRhoai:
		return rhoaiOperatorPackage
	default:
		return odhOperatorPackage
	}
}

// DeleteClusterExtension removes the ClusterExtension that installs packageName, if present.
func DeleteClusterExtension(ctx context.Context, cli client.Client, packageName string) error {
	ext, err := clusterExtensionForPackage(ctx, cli, packageName, "")
	if err != nil || ext == nil {
		return err
	}
	if err := cli.Delete(ctx, ext); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("error deleting ClusterExtension %s: %w", ext.Name, err)
	}
	return nil
}
