package plugins

import (
	"fmt"

	"sigs.k8s.io/kustomize/api/builtins" //nolint
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

func doAdjustReplicas(componentName string, resMap resmap.ResMap, value int) error {
	patch := fmt.Sprintf(`[{"op": "replace", "path": "/spec/replicas", "value": %v}]`, value)
	// set by pkg/plugins/addLabelsplugin.go:ApplyAddLabelsPlugin()
	label := "app.opendatahub.io/" + componentName + "=true"

	plug := builtins.PatchJson6902TransformerPlugin{
		Target: &types.Selector{
			ResId: resid.ResId{
				Gvk: resid.Gvk{Kind: "Deployment"},
			},
			LabelSelector: label,
		},
		JsonOp: patch,
	}

	fmt.Printf("Adjusting replicas, component %s, n %v\n", componentName, value)

	if err := plug.Transform(resMap); err != nil {
		return err
	}

	return nil
}

func AdjustReplicas(componentName string, resMap resmap.ResMap, c components.ComponentInterface) error {
	n := c.GetReplicas()
	if n == nil {
		return nil
	}

	return doAdjustReplicas(componentName, resMap, *n)
}
