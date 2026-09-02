package provision

import (
	"context"
	"errors"
	"fmt"

	"github.com/blang/semver/v4"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
)

// ResolveUpgradeGateVersions returns the target release used to match
// upgrade-gate keys. The release detected for the running operator is the
// primary source. DSC-owning CSVs provide a fallback and protect against OLM's
// transient two-CSV transition when the runtime release is stale.
func ResolveUpgradeGateVersions(
	ctx context.Context,
	reader client.Reader,
	namespace string,
	deployed common.Release,
	running common.Release,
) ([]string, error) {
	var target *semver.Version
	if isAtLeastVersion(running.Version.Version, deployed.Version.Version) {
		version := running.Version.Version
		target = &version
	}

	csvs := &operatorsv1alpha1.ClusterServiceVersionList{}
	if err := reader.List(ctx, csvs, client.InNamespace(namespace)); err != nil {
		if meta.IsNoMatchError(err) {
			return targetVersion(target)
		}
		return nil, fmt.Errorf("listing ClusterServiceVersions for upgrade gates: %w", err)
	}

	for i := range csvs.Items {
		csv := &csvs.Items[i]
		if !ownsDataScienceCluster(csv) || !isUpgradeTarget(csv.Spec.Version.Version, deployed.Version.Version) {
			continue
		}

		candidate := csv.Spec.Version.Version
		if target == nil || candidate.GT(*target) {
			candidateVersion := candidate
			target = &candidateVersion
		}
	}

	return targetVersion(target)
}

func targetVersion(target *semver.Version) ([]string, error) {
	if target == nil {
		return nil, errors.New("unable to determine target release for upgrade gates")
	}
	return []string{target.String()}, nil
}

func isVersionUpgrade(deployed semver.Version, target string) bool {
	if isZeroVersion(deployed) {
		return false
	}

	candidate, err := semver.Parse(target)
	return err == nil && candidate.GT(deployed)
}

func isAtLeastVersion(candidate, deployed semver.Version) bool {
	if isZeroVersion(candidate) {
		return false
	}
	if isZeroVersion(deployed) {
		return true
	}
	return !candidate.LT(deployed)
}

func isUpgradeTarget(candidate, deployed semver.Version) bool {
	if isZeroVersion(candidate) {
		return false
	}
	if isZeroVersion(deployed) {
		return true
	}
	return candidate.GT(deployed)
}

func isZeroVersion(version semver.Version) bool {
	return version.Major == 0 && version.Minor == 0 && version.Patch == 0
}

func ownsDataScienceCluster(csv *operatorsv1alpha1.ClusterServiceVersion) bool {
	for _, owned := range csv.Spec.CustomResourceDefinitions.Owned {
		if owned.Kind == "DataScienceCluster" {
			return true
		}
	}
	return false
}

func matchUpgradeGateKey(key string, versions []string) (string, bool) {
	for _, version := range versions {
		if gateName, matches := gates.MatchGateKey(key, version); matches {
			return gateName, true
		}
	}
	return "", false
}
