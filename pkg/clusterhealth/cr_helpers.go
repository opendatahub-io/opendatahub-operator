package clusterhealth

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UnhealthyChecker is the common signature for kind-specific condition checks.
// Receives the raw CR object and parsed conditions; returns messages for any conditions that count as unhealthy.
type UnhealthyChecker func(obj map[string]any, conditions []ConditionSummary) []string

// kindUnhealthyCheckers registers Kind-specific unhealthy logic. Kinds not in the map use defaultUnhealthyConditions (any condition not True).
// To add a new CR: add an entry here and optionally implement a small helper in this file.
var kindUnhealthyCheckers = map[string]UnhealthyChecker{
	"DataScienceCluster": dscUnhealthyConditions,
	"DSCInitialization":  dsciUnhealthyConditionsWithObj,
}

// getUnhealthyChecker returns the checker for the given Kind, or nil to use the default (any condition not True).
func getUnhealthyChecker(kind string) UnhealthyChecker {
	return kindUnhealthyCheckers[kind]
}

// dsciUnhealthyConditionsWithObj adapts dsciUnhealthyConditions to the UnhealthyChecker signature (DSCI logic ignores the object).
func dsciUnhealthyConditionsWithObj(_ map[string]any, conditions []ConditionSummary) []string {
	return dsciUnhealthyConditions(conditions)
}

// dscUnhealthyConditions returns condition strings that count as unhealthy for DSC.
// We only report unhealthy for a component if it is not Removed.
func dscUnhealthyConditions(obj map[string]any, conditions []ConditionSummary) []string {
	removed := dscRemovedComponentNames(obj)
	out := make([]string, 0, len(conditions))
	for _, cond := range conditions {
		if conditionStatusIsTrue(cond.Status) {
			continue
		}
		// Skip if the controller message says the component is Removed
		if conditionMessageIndicatesRemoved(cond.Message) {
			continue
		}
		// Check all possible spec keys (v1 vs v2 use different names, e.g. datasciencepipelines vs aipipelines).
		componentKeys := conditionTypeToDSCComponentKeys(cond.Type)
		skip := false
		for _, key := range componentKeys {
			if removed[key] {
				skip = true
				break
			}
		}
		if skip {
			continue // component is Removed; ignore its readiness
		}
		out = append(out, fmt.Sprintf("%s=%s", cond.Type, cond.Status))
	}
	return out
}

// conditionMessageIndicatesRemoved returns true when the condition message indicates we should not report (component Removed or dependency not managed).
func conditionMessageIndicatesRemoved(message string) bool {
	s := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(s, "managementstate is set to removed") {
		return true
	}
	// subcomponents like ModelsAsService have a dependency on the parent component (e.g. KServe)
	if strings.Contains(s, "is not managed") {
		return true
	}
	return false
}

// dscRemovedComponentNames returns the set of component keys (e.g. "dashboard", "kueue") that have managementState "Removed".
// Reads from both spec.components and status.components (keys stored lowercase for lookup).
func dscRemovedComponentNames(obj map[string]any) map[string]bool {
	removed := make(map[string]bool)
	for _, path := range [][]string{{"spec", "components"}, {"status", "components"}} {
		components, found, _ := unstructured.NestedMap(obj, path...)
		if !found {
			continue
		}
		for name, val := range components {
			comp, ok := val.(map[string]any)
			if !ok {
				continue
			}
			ms, _, _ := unstructured.NestedString(comp, "managementState")
			if ms == "" {
				ms, _, _ = unstructured.NestedString(comp, "managementSpec", "managementState")
			}
			if strings.EqualFold(strings.TrimSpace(ms), "Removed") {
				removed[strings.ToLower(strings.TrimSpace(name))] = true
			}
		}
	}
	return removed
}

// conditionTypeToDSCComponentKeys returns all possible spec.components keys for this condition type.
// DSC v1 and v2 use different JSON keys for the same component (e.g. "datasciencepipelines" vs "aipipelines"),
// so we check all possibilities when deciding if a component is Removed.
func conditionTypeToDSCComponentKeys(conditionType string) []string {
	s := strings.TrimSpace(conditionType)
	if s == "Ready" || s == "ComponentsReady" {
		return nil
	}
	if !strings.HasSuffix(s, "Ready") {
		return nil
	}
	base := strings.ToLower(s[:len(s)-len("Ready")])
	// v1 uses "datasciencepipelines", v2 uses "aipipelines"; condition can be DataSciencePipelinesReady or AIPipelinesReady.
	if base == "datasciencepipelines" || base == "aipipelines" {
		return []string{"datasciencepipelines", "aipipelines"}
	}
	return []string{base}
}

// dsciUnhealthyConditions returns condition strings that count as unhealthy for DSCI.
// We ignore Progressing and Upgradeable. We report unhealthy only when Degraded is True.
func dsciUnhealthyConditions(conditions []ConditionSummary) []string {
	out := make([]string, 0, len(conditions))
	for _, cond := range conditions {
		switch strings.TrimSpace(cond.Type) {
		case "Progressing", "Upgradeable":
			continue
		case "Degraded":
			if conditionStatusIsTrue(cond.Status) {
				out = append(out, fmt.Sprintf("%s=%s", cond.Type, cond.Status))
			}
			continue
		}
		if !conditionStatusIsTrue(cond.Status) {
			out = append(out, fmt.Sprintf("%s=%s", cond.Type, cond.Status))
		}
	}
	return out
}
