package clusterhealth

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CR conditions health check: one generic flow — fetch CR by GVK, sanity checks, then Kind-specific unhealthy logic.
// (1) Fetch by GVK (direct Get if name set, else singleton discovery via List).
// (2) Common sanity: handle errors, not found, parse status.conditions.
// (3) Kind-specific unhealthy checks via registry in cr_helpers.go (default: any condition not True).
// To add a new CR: call runCRConditionsSection from run.go with its GVK and nn; register an UnhealthyChecker in cr_helpers if needed.

// runCRConditionsSection is the single generic runner for CR condition health checks.
func runCRConditionsSection(ctx context.Context, c client.Client, objGVK schema.GroupVersionKind, nn types.NamespacedName) SectionResult[CRConditionsSection] {
	var out SectionResult[CRConditionsSection]
	obj, err := getCRByGVK(ctx, c, objGVK, nn)
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

	conditions, err := parseConditionsFromUnstructured(obj.Object)
	if err != nil {
		out.Error = fmt.Sprintf("failed to parse %s status.conditions: %v", out.Data.Name, err)
		out.Data.Conditions = []ConditionSummary{}
		return out
	}
	if conditions == nil {
		conditions = []ConditionSummary{}
	}
	out.Data.Conditions = conditions
	out.Data.Data = obj

	var unhealthyMsgs []string
	if checker := getUnhealthyChecker(objGVK.Kind); checker != nil {
		unhealthyMsgs = checker(obj.Object, conditions)
	} else {
		unhealthyMsgs = defaultUnhealthyConditions(conditions)
	}
	if len(unhealthyMsgs) > 0 {
		out.Error = fmt.Sprintf("%s unhealthy conditions: %s", out.Data.Name, strings.Join(unhealthyMsgs, "; "))
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

// getCRByGVK fetches a CR by GroupVersionKind and optional namespaced name.
// If nn.Name is set, it does a direct Get. If nn.Name is empty, it lists the Kind and returns the first item (singleton discovery).
// Caller handles NotFound, list empty, and other errors.
func getCRByGVK(ctx context.Context, c client.Client, objGVK schema.GroupVersionKind, nn types.NamespacedName) (*unstructured.Unstructured, error) {
	if nn.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(objGVK)
		err := c.Get(ctx, nn, obj)
		if err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				if nn.Namespace != "" {
					return nil, fmt.Errorf("CR %s not found in namespace %s", nn.Name, nn.Namespace)
				}
				return nil, fmt.Errorf("CR %s not found", nn.Name)
			}
			return nil, fmt.Errorf("failed to get CR: %w", err)
		}
		return obj, nil
	}
	// Singleton discovery: list all and take the first.
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   objGVK.Group,
		Version: objGVK.Version,
		Kind:    objGVK.Kind + "List",
	})
	if err := c.List(ctx, list); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil // Kind not registered, treat as not found
		}
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

// parseConditionsFromUnstructured extracts status.conditions from an unstructured object.
// It returns (nil, nil) when status.conditions is absent (not found); callers treat that as "no conditions".
// It returns (nil, err) when status.conditions exists but is malformed (e.g. not a slice), so callers can
// report a parsing error instead of treating the CR as healthy.
func parseConditionsFromUnstructured(obj map[string]any) ([]ConditionSummary, error) {
	conditions, found, err := unstructured.NestedSlice(obj, "status", "conditions")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	out := make([]ConditionSummary, 0, len(conditions))
	for _, raw := range conditions {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		condType, _ := m["type"].(string)
		status, _ := m["status"].(string)
		message, _ := m["message"].(string)
		out = append(out, ConditionSummary{Type: condType, Status: status, Message: message})
	}
	return out, nil
}
