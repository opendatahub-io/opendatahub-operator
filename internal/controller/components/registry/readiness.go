package registry

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// ComponentReadinessChecker implements dag.ReadinessChecker for
// in-tree components. It lists component CRs on cluster by GVK
// (singleton — at most 1) and checks the Ready condition and
// platform release version handshake.
type ComponentReadinessChecker struct {
	registry      *Registry
	client        client.Client
	targetVersion string
}

// NewReadinessChecker creates a ReadinessChecker backed by this
// component registry. The targetVersion is the current operator
// version — components must have a matching platform release entry
// in .status.releases to be considered ready during upgrades.
func NewReadinessChecker(reg *Registry, cli client.Client, targetVersion string) *ComponentReadinessChecker {
	return &ComponentReadinessChecker{
		registry:      reg,
		client:        cli,
		targetVersion: targetVersion,
	}
}

// IsReady returns true if the named component's CR has Ready=True
// on the cluster with a matching platform release version.
// If no CR exists (not deployed or disabled), the component is
// treated as ready (don't block DAG advancement).
func (c *ComponentReadinessChecker) IsReady(ctx context.Context, name string) (bool, error) {
	handler := c.registry.Lookup(name)
	if handler == nil {
		return false, fmt.Errorf("component %q: %w", name, dag.ErrUnknownNode)
	}

	gvk := handler.GroupVersionKind()

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := c.client.List(ctx, list); err != nil {
		if k8serr.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("component %q: failed to list CRs: %w", name, err)
	}

	if len(list.Items) == 0 {
		return true, nil
	}

	obj := &list.Items[0]

	if c.targetVersion != "" {
		rv := extractPlatformReleaseVersion(obj)
		// Platform release version is only set when the component reaches
		// Ready=True (see reconciler.go setPlatformRelease). A matching
		// version proves the component was successfully reconciled at this
		// version — skip the Ready check since transient failures (crashloop,
		// missing secret) are unrelated to upgrade ordering.
		// Empty rv means the component was never tracked (pre-feature CR
		// or first deploy) — no upgrade to block on.
		return rv == "" || rv == c.targetVersion, nil
	}

	return isUnstructuredReady(obj), nil
}

const platformReleaseName = common.PlatformReleaseName

func extractPlatformReleaseVersion(u *unstructured.Unstructured) string {
	releases, found, _ := unstructured.NestedSlice(u.Object, "status", "releases")
	if !found {
		return ""
	}

	for _, item := range releases {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(entry, "name")
		if name == platformReleaseName {
			ver, _, _ := unstructured.NestedString(entry, "version")
			return ver
		}
	}

	return ""
}

func isUnstructuredReady(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == status.ConditionTypeReady && cond["status"] == string(metav1.ConditionTrue) {
			return true
		}
	}

	return false
}
