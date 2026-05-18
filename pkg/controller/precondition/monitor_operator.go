package precondition

import (
	"context"
	"errors"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ConditionFilterFunc evaluates an operator CR's status condition and returns true
// if the condition indicates an unhealthy state that should be reported.
type ConditionFilterFunc func(conditionType string, status string) bool

// OperatorConfig defines the domain parameters for monitoring an external operator CR.
type OperatorConfig struct {
	// OperatorGVK is the GVK of the operator CR to monitor. Required.
	OperatorGVK schema.GroupVersionKind

	// CRName is the name of the operator CR to fetch. Optional: when empty,
	// the first CR found via list is used (selection may be arbitrary if multiple exist).
	CRName string

	// CRNamespace is the namespace of the operator CR. Optional: leave empty
	// for cluster-scoped resources.
	CRNamespace string

	// Filter evaluates each status condition on the operator CR and returns true
	// for conditions that should be reported as unhealthy. Required: must not be nil.
	Filter ConditionFilterFunc

	// RequireCR controls behavior when the CRD is registered but no CR exists.
	// When false (default), a missing CR is treated as healthy (operator not active).
	// When true, a missing CR is reported as unhealthy.
	// A missing CRD always passes regardless of this setting (operator not installed).
	RequireCR bool
}

var errOperatorCRNotFound = errors.New("operator CR not found")

// MonitorOperator creates a PreCondition that checks an external operator's health
// by reading its CR's status conditions and applying the configured Filter.
// See [OperatorConfig] for configuration details including missing CRD/CR behavior.
func MonitorOperator(config OperatorConfig, opts ...Option) PreCondition {
	return newPreCondition(func(ctx context.Context, rr *odhtypes.ReconciliationRequest) (CheckResult, error) {
		if config.OperatorGVK == (schema.GroupVersionKind{}) {
			return CheckResult{}, errors.New("MonitorOperator: OperatorGVK must not be empty")
		}
		if config.Filter == nil {
			return CheckResult{}, errors.New("MonitorOperator: Filter must not be nil")
		}

		cr, err := fetchOperatorCR(ctx, rr.Client, config)
		if err != nil {
			if meta.IsNoMatchError(err) {
				return CheckResult{Pass: true}, nil
			}
			if k8serr.IsNotFound(err) || errors.Is(err, errOperatorCRNotFound) {
				if config.RequireCR {
					return CheckResult{
						Pass:    false,
						Message: fmt.Sprintf("%s: operator CR not found", operatorIdentifier(config)),
					}, nil
				}

				return CheckResult{Pass: true}, nil
			}

			return CheckResult{}, fmt.Errorf("%s: failed to get operator CR: %w", operatorIdentifier(config), err)
		}

		degraded, err := collectDegradedConditions(ctx, cr, config)
		if err != nil {
			return CheckResult{}, fmt.Errorf("%s: failed to parse conditions: %w", operatorIdentifier(config), err)
		}

		if len(degraded) > 0 {
			return CheckResult{Pass: false, Message: strings.Join(degraded, "; ")}, nil
		}

		l := ctrlLog.FromContext(ctx)
		l.V(1).Info("Operator dependency check passed", "gvk", config.OperatorGVK.String(), "cr", cr.GetName())

		return CheckResult{Pass: true}, nil
	}, opts...)
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

	// Limit to 2: enough to detect multiple CRs (logged as warning) without fetching the full list.
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

func collectDegradedConditions(ctx context.Context, cr *unstructured.Unstructured, config OperatorConfig) ([]string, error) {
	conditions, found, err := unstructured.NestedSlice(cr.Object, "status", "conditions")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	crIdentifier := cr.GetName()
	if config.CRNamespace != "" {
		crIdentifier = fmt.Sprintf("%s/%s", config.CRNamespace, crIdentifier)
	}

	degraded := make([]string, 0, len(conditions))

	for _, c := range conditions {
		condMap, ok := c.(map[string]any)
		if !ok {
			l := ctrlLog.FromContext(ctx)
			l.V(1).Info("Skipping malformed condition entry in operator CR status",
				"gvk", config.OperatorGVK.String(),
				"value", fmt.Sprintf("%v", c))

			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")

		if condType == "" || condStatus == "" {
			l := ctrlLog.FromContext(ctx)
			l.V(1).Info("Skipping condition with missing type or status",
				"gvk", config.OperatorGVK.String(),
				"type", condType,
				"status", condStatus)

			continue
		}

		if !config.Filter(condType, condStatus) {
			continue
		}

		reason, _, _ := unstructured.NestedString(condMap, "reason")
		message, _, _ := unstructured.NestedString(condMap, "message")

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

		degraded = append(degraded, condDetail)
	}

	return degraded, nil
}
