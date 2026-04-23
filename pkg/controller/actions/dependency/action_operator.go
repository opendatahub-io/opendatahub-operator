package dependency

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	dependencyDegradedReason = "DependencyDegraded"

	degradedConditionType  = "Degraded"
	availableConditionType = "Available"
	readyConditionType     = "Ready"
)

// errOperatorCRNotFound is returned by getFirstCR when the CR list is empty.
var errOperatorCRNotFound = errors.New("operator CR not found")

// DegradedConditionFilterFunc defines a function that returns true if the condition indicates a degraded state.
type DegradedConditionFilterFunc func(conditionType string, status string) bool

// OperatorConfig defines configuration for monitoring a dependent operator.
type OperatorConfig struct {
	// OperatorGVK is the GVK of the dependent operator CR
	OperatorGVK schema.GroupVersionKind

	// CRName is the name of the dependent operator CR.
	// If left empty, the action will monitor the first CR in the namespace.
	CRName string

	// CRNamespace is the namespace where the operator CR lives.
	// Leave empty for cluster-scoped resources.
	CRNamespace string

	// Filter allows customizing how degraded conditions are evaluated.
	// If nil, DefaultDegradedConditionFilter is used.
	Filter DegradedConditionFilterFunc

	// ClusterTypes restricts this check to run only on specific cluster types
	// (e.g. cluster.ClusterTypeOpenShift). If empty, the check runs on all
	// cluster types.
	ClusterTypes []string

	// Severity determines how degraded conditions affect component readiness.
	// Use ConditionSeverityError ("") for required dependencies (affects Ready).
	// Use ConditionSeverityInfo for optional dependencies (informational only).
	// Default: ConditionSeverityError
	Severity common.ConditionSeverity
}

// CRDConfig defines configuration for checking that a required CRD is registered on the cluster.
type CRDConfig struct {
	// GVK identifies the CRD to check for cluster registration.
	GVK schema.GroupVersionKind

	// ClusterTypes restricts this check to run only on specific cluster types
	// (e.g. cluster.ClusterTypeOpenShift). If empty, the check runs on all
	// cluster types.
	ClusterTypes []string

	// Severity determines how a missing CRD affects component readiness.
	// Use ConditionSeverityError (default) for required CRDs that block the component.
	// Use ConditionSeverityInfo for optional CRDs where absence is informational only.
	Severity common.ConditionSeverity
}

// action monitors dependent operators for degraded conditions and propagates them to the component CR.
type action struct {
	configs    []OperatorConfig
	crdConfigs []CRDConfig
}

// ActionOpts is a functional option for configuring the dependency action.
type ActionOpts func(*action)

// MonitorOperator adds an operator to monitor for degraded conditions.
func MonitorOperator(config OperatorConfig) ActionOpts {
	return func(a *action) {
		a.configs = append(a.configs, config)
	}
}

// MonitorCRD adds a CRD presence check to the action. If the specified CRD is absent
// from the cluster, DependenciesAvailable is set to False.
func MonitorCRD(config CRDConfig) ActionOpts {
	return func(a *action) {
		a.crdConfigs = append(a.crdConfigs, config)
	}
}

// Combine returns a single ActionOpts that applies all given opts in order.
func Combine(opts ...ActionOpts) ActionOpts {
	return func(a *action) {
		for _, opt := range opts {
			opt(a)
		}
	}
}

// run propagates upstream operator health to component status.
// It aggregates degraded conditions from all configured operators into a single
// DependenciesAvailable condition, allowing users to see upstream failures
// that may be blocking their component from working correctly.
func (a *action) run(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	var allDegraded []string
	hasErrorSeverity := false
	clusterType := cluster.GetClusterInfo().Type

	for _, config := range a.configs {
		if len(config.ClusterTypes) > 0 && !slices.Contains(config.ClusterTypes, clusterType) {
			continue
		}

		degraded := a.collectDegradedConditions(ctx, rr, config)
		if len(degraded) > 0 {
			allDegraded = append(allDegraded, degraded...)
			if config.Severity != common.ConditionSeverityInfo {
				hasErrorSeverity = true
			}
		}
	}

	for _, config := range a.crdConfigs {
		if len(config.ClusterTypes) > 0 && !slices.Contains(config.ClusterTypes, clusterType) {
			continue
		}

		has, err := cluster.HasCRD(ctx, rr.Client, config.GVK)
		if err != nil {
			// Log and continue - monitoring failures should not block reconciliation.
			logger := ctrlLog.FromContext(ctx)
			logger.V(3).Info("Failed to check CRD presence for dependency monitoring",
				"gvk", config.GVK.String(),
				"error", err.Error())
			continue
		}
		if !has {
			allDegraded = append(allDegraded, config.GVK.Kind+": CRD not found")
			if config.Severity != common.ConditionSeverityInfo {
				hasErrorSeverity = true
			}
		}
	}

	if len(allDegraded) > 0 {
		severity := common.ConditionSeverityInfo
		if hasErrorSeverity {
			severity = common.ConditionSeverityError
		}
		rr.Conditions.MarkFalse(
			status.ConditionDependenciesAvailable,
			cond.WithSeverity(severity),
			cond.WithReason(dependencyDegradedReason),
			cond.WithMessage("Dependencies degraded: %s", strings.Join(allDegraded, "; ")),
		)
	} else {
		rr.Conditions.MarkTrue(status.ConditionDependenciesAvailable)
	}

	return nil
}

// collectDegradedConditions detects when an external operator dependency is unhealthy.
// Returns an empty slice if the operator is healthy, not installed, or cannot be checked.
// Errors are logged instead of returned so that monitoring failures do not break reconciliation.
func (a *action) collectDegradedConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest, config OperatorConfig) []string {
	externalCR := &unstructured.Unstructured{}
	externalCR.SetGroupVersionKind(config.OperatorGVK)

	var err error
	if config.CRName == "" {
		err = a.getFirstCR(ctx, rr, config, externalCR)
	} else {
		err = rr.Client.Get(ctx, types.NamespacedName{
			Name:      config.CRName,
			Namespace: config.CRNamespace,
		}, externalCR)
	}

	if meta.IsNoMatchError(err) {
		return nil
	}
	if k8serr.IsNotFound(err) || errors.Is(err, errOperatorCRNotFound) {
		// Operator CR absent but CRD is registered — not degraded
		return nil
	}
	if err != nil {
		// Log and continue - monitoring failures should not break reconciliation
		logger := ctrlLog.FromContext(ctx)
		logger.V(3).Info("Failed to get operator CR for dependency monitoring",
			"gvk", config.OperatorGVK.String(),
			"error", err.Error())
		return nil
	}

	// Extract operator health conditions to detect degradation patterns
	conditions, found, err := unstructured.NestedSlice(externalCR.Object, "status", "conditions")
	if err != nil {
		// Log and continue - malformed status should not break reconciliation
		logger := ctrlLog.FromContext(ctx)
		logger.V(3).Info("Failed to parse conditions from operator CR",
			"gvk", config.OperatorGVK.String(),
			"error", err.Error())
		return nil
	}
	if !found {
		// No conditions - operator is healthy
		return nil
	}

	filter := config.Filter
	if filter == nil {
		filter = DefaultDegradedConditionFilter
	}

	// Aggregate failing conditions for the user-facing error message.

	// Include the specific CR name (namespace/name when available) to make the
	// surfaced dependency error actionable if multiple CRs of the same Kind are present.
	crIdentifier := externalCR.GetName()
	if config.CRNamespace != "" && crIdentifier != "" {
		crIdentifier = fmt.Sprintf("%s/%s", config.CRNamespace, crIdentifier)
	}

	var degradedConditions []string
	for _, c := range conditions {
		condMap, ok := c.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")

		if filter(condType, condStatus) {
			reason, _, _ := unstructured.NestedString(condMap, "reason")
			message, _, _ := unstructured.NestedString(condMap, "message")

			// Include operator name and CR identifier so users know more explicitly which dependency failed
			condPrefix := config.OperatorGVK.Kind
			if crIdentifier != "" {
				condPrefix = fmt.Sprintf("%s %s", condPrefix, crIdentifier)
			}
			condDetail := fmt.Sprintf("%s: %s=%s", condPrefix, condType, condStatus)
			if reason != "" {
				condDetail += fmt.Sprintf(" (%s)", reason)
			}
			if message != "" {
				condDetail += fmt.Sprintf(": %s", message)
			}
			degradedConditions = append(degradedConditions, condDetail)
		}
	}

	return degradedConditions
}

func (a *action) getFirstCR(ctx context.Context, rr *odhtypes.ReconciliationRequest, config OperatorConfig, out *unstructured.Unstructured) error {
	// Support both namespace-scoped and cluster-scoped operator CRs:
	// namespace-scoped operators set CRNamespace, cluster-scoped ones leave it empty.
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(config.OperatorGVK)

	// Use a minimal limit to detect when multiple CRs exist without pulling large lists.
	// If more than one is returned, selection is arbitrary and we log a warning further down.
	lo := []client.ListOption{client.Limit(2)}
	if config.CRNamespace != "" {
		lo = append(lo, client.InNamespace(config.CRNamespace))
	}

	// Note: without a CRName we rely on the API server returning “some” item.
	// Limit avoids large list payloads; order is undefined, so this is a
	// best-effort fallback for cases where callers do not specify CRName.
	// The alternative would be to fetch all items, but that could be expensive.
	if err := rr.Client.List(ctx, list, lo...); err != nil {
		return err
	}

	if len(list.Items) == 0 {
		return errOperatorCRNotFound
	}

	// If multiple CRs are present, selection is arbitrary; log a warning.
	if len(list.Items) > 1 {
		logger := ctrlLog.FromContext(ctx)
		logger.Info("Dependency monitoring found multiple CRs; relying on first returned item (selection may be arbitrary)",
			"gvk", config.OperatorGVK.String(),
			"namespace", config.CRNamespace)
	}

	*out = list.Items[0]
	return nil
}

// DefaultDegradedConditionFilter handles some standard Kubernetes operator health patterns.
func DefaultDegradedConditionFilter(condType, condStatus string) bool {
	if condType == degradedConditionType && condStatus == string(metav1.ConditionTrue) {
		return true
	}
	if (condType == availableConditionType || condType == readyConditionType) && condStatus == string(metav1.ConditionFalse) {
		return true
	}

	return false
}

// NewAction creates an action that monitors external operator health and propagates
// degraded conditions to the caller's DependenciesAvailable status condition.
func NewAction(opts ...ActionOpts) actions.Fn {
	a := action{}

	for _, opt := range opts {
		opt(&a)
	}

	return a.run
}
