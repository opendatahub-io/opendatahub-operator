package clusterhealth

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func runDSCISection(ctx context.Context, c client.Client, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	return runCRConditionsSection(ctx, c, gvk.DSCInitialization, nn)
}

func runDSCSection(ctx context.Context, c client.Client, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	return runCRConditionsSection(ctx, c, gvk.DataScienceCluster, nn)
}

func runCRConditionsSection(ctx context.Context, c client.Client, objGVK schema.GroupVersionKind, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	var out SectionResult[CRConditionsSection]
	// DSCI and DSC are singletons; if no name is given, discover the single instance on the cluster.
	obj, err := getDSCIOrDSCCR(ctx, c, objGVK, nn)
	if err != nil {
		out.Error = err.Error()
		out.Data.Conditions = []ConditionSummary{}
		return out
	}
	if obj == nil {
		out.Error = fmt.Sprintf("no %s found on cluster", objGVK.Kind)
		out.Data.Conditions = []ConditionSummary{}
		return out
	}
	out.Data.Name = obj.GetName()

	conditions := parseConditionsFromUnstructured(obj.Object)
	if conditions == nil {
		conditions = []ConditionSummary{}
	}
	out.Data.Conditions = conditions
	out.Data.Data = obj

	var unhealthyMsgs []string
	switch objGVK.Kind {
	case "DataScienceCluster":
		unhealthyMsgs = dscUnhealthyConditions(obj.Object, conditions)
	case "DSCInitialization":
		unhealthyMsgs = dsciUnhealthyConditions(conditions)
	default:
		unhealthyMsgs = defaultUnhealthyConditions(conditions)
	}
	if len(unhealthyMsgs) > 0 {
		out.Error = fmt.Sprintf("%s unhealthy conditions: %s", out.Data.Name, strings.Join(unhealthyMsgs, "; "))
	}
	return out
}

// dscUnhealthyConditions returns condition strings that count as unhealthy for DSC.
// We only report unhealthy for a component if it is not Removed.
func dscUnhealthyConditions(obj map[string]interface{}, conditions []ConditionSummary) []string {
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
func dscRemovedComponentNames(obj map[string]interface{}) map[string]bool {
	removed := make(map[string]bool)
	for _, path := range [][]string{{"spec", "components"}, {"status", "components"}} {
		components, found, _ := unstructured.NestedMap(obj, path...)
		if !found {
			continue
		}
		for name, val := range components {
			comp, ok := val.(map[string]interface{})
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

// defaultUnhealthyConditions returns any condition that is not True (for unknown CR kinds).
func defaultUnhealthyConditions(conditions []ConditionSummary) []string {
	out := make([]string, 0, len(conditions))
	for _, cond := range conditions {
		if !conditionStatusIsTrue(cond.Status) {
			out = append(out, fmt.Sprintf("%s=%s", cond.Type, cond.Status))
		}
	}
	return out
}

func getDSCIOrDSCCR(ctx context.Context, c client.Client, objGVK schema.GroupVersionKind, nn types.NamespacedName) (*unstructured.Unstructured, error) {
	if nn.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(objGVK)
		err := c.Get(ctx, nn, obj)
		if err != nil {
			if k8serr.IsNotFound(err) {
				if nn.Namespace != "" {
					return nil, fmt.Errorf("CR %s not found in namespace %s", nn.Name, nn.Namespace)
				}
				return nil, fmt.Errorf("CR %s not found", nn.Name)
			}
			return nil, fmt.Errorf("failed to get CR: %w", err)
		}
		return obj, nil
	}
	// Discover singleton: list all and take the first.
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   objGVK.Group,
		Version: objGVK.Version,
		Kind:    objGVK.Kind + "List",
	})
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", objGVK.Kind, err)
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	return &list.Items[0], nil
}

func conditionStatusIsTrue(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "True")
}

func parseConditionsFromUnstructured(obj map[string]interface{}) []ConditionSummary {
	conditions, found, _ := unstructured.NestedSlice(obj, "status", "conditions")
	if !found {
		return nil
	}
	out := make([]ConditionSummary, 0, len(conditions))
	for _, raw := range conditions {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := m["type"].(string)
		status, _ := m["status"].(string)
		message, _ := m["message"].(string)
		out = append(out, ConditionSummary{Type: condType, Status: status, Message: message})
	}
	return out
}
