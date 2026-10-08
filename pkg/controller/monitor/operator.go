package monitor

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
)

// ConditionFilterFunc evaluates an operator CR's status condition and returns true
// if the condition indicates an unhealthy state that should be reported.
type ConditionFilterFunc func(conditionType string, status string) bool

// RequiredCondition declares a status condition that must be present on the
// operator CR with a specific status for the dependency to be reported healthy.
//
// Requiring positive conditions is not the same as the absence of negative ones:
// an operator that has never reported Available=True is not healthy just because
// it has not reported Degraded=True either.
type RequiredCondition struct {
	Type string

	Status string
}

// OperatorConfig defines the domain parameters for monitoring an external operator CR.
type OperatorConfig struct {
	OperatorGVK schema.GroupVersionKind

	// CRName is the name of the operator CR to fetch. Optional: when empty,
	// the first CR found via list is used (selection may be arbitrary if multiple exist).
	CRName string

	// Leave CRNamespace empty for a cluster-scoped resource.
	CRNamespace string

	// Filter reports unhealthy conditions. At least one of Filter and
	// RequiredConditions must be configured.
	Filter ConditionFilterFunc

	// RequiredConditions require positive evidence; missing or Unknown conditions
	// are indeterminate, while a different definite status is a failure.
	RequiredConditions []RequiredCondition

	// RequireCR reports a missing CR as unhealthy. A missing CRD passes by
	// default; callers that already confirmed CRD presence can set NoMatchAsUnknown.
	RequireCR bool

	// NoMatchAsUnknown is for callers that have already confirmed the CRD exists
	// by name. If discovery has not caught up yet, an unmapped GVK is incomplete
	// evidence rather than proof that the optional operator is absent.
	NoMatchAsUnknown bool
}

var errOperatorCRNotFound = errors.New("operator CR not found")

// CheckOperatorHealth evaluates an external operator CR against [OperatorConfig].
func CheckOperatorHealth(ctx context.Context, cli client.Client, config OperatorConfig) (CheckResult, error) {
	if config.OperatorGVK == (schema.GroupVersionKind{}) {
		return CheckResult{}, errors.New("CheckOperatorHealth: OperatorGVK must not be empty")
	}
	if config.Filter == nil && len(config.RequiredConditions) == 0 {
		return CheckResult{}, errors.New("CheckOperatorHealth: at least one of Filter or RequiredConditions must be set")
	}
	for _, rc := range config.RequiredConditions {
		if rc.Type == "" || rc.Status == "" {
			return CheckResult{}, errors.New("CheckOperatorHealth: RequiredCondition Type and Status must not be empty")
		}
	}

	cr, err := fetchOperatorCR(ctx, cli, config)
	if err != nil {
		if meta.IsNoMatchError(err) {
			if config.NoMatchAsUnknown {
				return Indeterminate("%s: operator API not yet available in discovery", operatorIdentifier(config)), nil
			}

			return Passed(), nil
		}
		if k8serr.IsNotFound(err) || errors.Is(err, errOperatorCRNotFound) {
			if config.RequireCR {
				return Failed("%s: operator CR not found", operatorIdentifier(config)), nil
			}

			return Passed(), nil
		}

		return CheckResult{}, fmt.Errorf("%s: failed to get operator CR: %w", operatorIdentifier(config), err)
	}

	conditions, err := readConditions(ctx, cr, config)
	if err != nil {
		// A status block that cannot be parsed is missing information, not proof of
		// failure. Report it as indeterminate so the caller requeues rather than
		// declaring the dependency broken.
		return Indeterminate("%s: failed to parse status conditions: %s", operatorIdentifier(config), err.Error()), nil
	}

	failed := collectDegradedConditions(cr, config, conditions)
	unmet, indeterminate := evaluateRequiredConditions(cr, config, conditions)

	failed = append(failed, unmet...)

	if len(failed) > 0 {
		// Surface indeterminate findings alongside failures so the operator sees the
		// full picture, but the outcome is a definite failure.
		return Failed("%s", strings.Join(append(failed, indeterminate...), "; ")), nil
	}

	if len(indeterminate) > 0 {
		return Indeterminate("%s", strings.Join(indeterminate, "; ")), nil
	}

	l := ctrlLog.FromContext(ctx)
	l.V(1).Info("Operator dependency check passed", "gvk", config.OperatorGVK.String(), "cr", cr.GetName())

	return Passed(), nil
}

func fetchOperatorCR(ctx context.Context, cli client.Client, config OperatorConfig) (*unstructured.Unstructured, error) {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(config.OperatorGVK)

	if config.CRName != "" {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      config.CRName,
			Namespace: config.CRNamespace,
		}, cr)

		return cr, err
	}

	return getFirstCR(ctx, cli, config)
}

func getFirstCR(ctx context.Context, cli client.Client, config OperatorConfig) (*unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(config.OperatorGVK)

	lo := []client.ListOption{client.Limit(2)}
	if config.CRNamespace != "" {
		lo = append(lo, client.InNamespace(config.CRNamespace))
	}

	if err := cli.List(ctx, list, lo...); err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, errOperatorCRNotFound
	}

	if len(list.Items) > 1 {
		l := ctrlLog.FromContext(ctx)
		l.Info("Dependency monitoring found multiple CRs; relying on first returned item (selection may be arbitrary)",
			"gvk", config.OperatorGVK.String(),
			"namespace", config.CRNamespace)
	}

	return &list.Items[0], nil
}

func operatorIdentifier(config OperatorConfig) string {
	id := config.OperatorGVK.Kind
	if config.CRName != "" {
		if config.CRNamespace != "" {
			id = fmt.Sprintf("%s %s/%s", id, config.CRNamespace, config.CRName)
		} else {
			id = fmt.Sprintf("%s %s", id, config.CRName)
		}
	}

	return id
}

type crCondition struct {
	status  string
	reason  string
	message string
}

// readConditions ignores malformed entries that carry no usable signal. It rejects
// duplicate types because choosing one conflicting state would be arbitrary.
func readConditions(ctx context.Context, cr *unstructured.Unstructured, config OperatorConfig) (map[string]crCondition, error) {
	conditions, found, err := unstructured.NestedSlice(cr.Object, "status", "conditions")
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]crCondition{}, nil
	}

	l := ctrlLog.FromContext(ctx)
	parsed := make(map[string]crCondition, len(conditions))

	for _, c := range conditions {
		condMap, ok := c.(map[string]any)
		if !ok {
			l.V(1).Info("Skipping malformed condition entry in operator CR status",
				"gvk", config.OperatorGVK.String(),
				"value", fmt.Sprintf("%v", c))

			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")

		if condType == "" || condStatus == "" {
			l.V(1).Info("Skipping condition with missing type or status",
				"gvk", config.OperatorGVK.String(),
				"type", condType,
				"status", condStatus)

			continue
		}

		reason, _, _ := unstructured.NestedString(condMap, "reason")
		message, _, _ := unstructured.NestedString(condMap, "message")
		if _, duplicate := parsed[condType]; duplicate {
			return nil, fmt.Errorf("duplicate condition type %q", condType)
		}

		parsed[condType] = crCondition{status: condStatus, reason: reason, message: message}
	}

	return parsed, nil
}

func collectDegradedConditions(cr *unstructured.Unstructured, config OperatorConfig, conditions map[string]crCondition) []string {
	if config.Filter == nil {
		return nil
	}

	degraded := make([]string, 0, len(conditions))

	// Iterate over a sorted key set so the message is stable across reconciliations
	// and does not cause spurious status updates.
	for _, condType := range slices.Sorted(maps.Keys(conditions)) {
		cond := conditions[condType]
		if !config.Filter(condType, cond.status) {
			continue
		}

		degraded = append(degraded, conditionDetail(cr, config, condType, cond))
	}

	return degraded
}

// evaluateRequiredConditions separates definite failures from indeterminate
// findings so callers can give definite failures precedence.
func evaluateRequiredConditions(
	cr *unstructured.Unstructured,
	config OperatorConfig,
	conditions map[string]crCondition,
) ([]string, []string) {
	var unmet, indeterminate []string

	prefix := crPrefix(cr, config)

	for _, rc := range config.RequiredConditions {
		cond, found := conditions[rc.Type]

		switch {
		case !found:
			// Either the condition was never reported or its entry was malformed.
			// Both mean we cannot tell whether the dependency is healthy.
			indeterminate = append(indeterminate,
				fmt.Sprintf("%s: required condition %s not found", prefix, rc.Type))

		case cond.status != string(metav1.ConditionTrue) && cond.status != string(metav1.ConditionFalse):
			indeterminate = append(indeterminate, conditionDetail(cr, config, rc.Type, cond))

		case cond.status != rc.Status:
			unmet = append(unmet, fmt.Sprintf("%s (expected %s=%s)",
				conditionDetail(cr, config, rc.Type, cond), rc.Type, rc.Status))
		}
	}

	return unmet, indeterminate
}

func crPrefix(cr *unstructured.Unstructured, config OperatorConfig) string {
	crIdentifier := cr.GetName()
	if config.CRNamespace != "" {
		crIdentifier = fmt.Sprintf("%s/%s", config.CRNamespace, crIdentifier)
	}

	if crIdentifier == "" {
		return config.OperatorGVK.Kind
	}

	return fmt.Sprintf("%s %s", config.OperatorGVK.Kind, crIdentifier)
}

func conditionDetail(cr *unstructured.Unstructured, config OperatorConfig, condType string, cond crCondition) string {
	detail := fmt.Sprintf("%s: %s=%s", crPrefix(cr, config), condType, cond.status)
	if cond.reason != "" {
		detail += fmt.Sprintf(" (%s)", cond.reason)
	}
	if cond.message != "" {
		detail += fmt.Sprintf(": %s", cond.message)
	}

	return detail
}
