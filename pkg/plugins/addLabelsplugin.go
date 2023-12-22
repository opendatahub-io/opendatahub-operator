package plugins

import (
	"sigs.k8s.io/kustomize/api/builtins" //nolint
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
)

func ApplyAddLabelsPlugin(componentName string, resMap resmap.ResMap) error {
	nsplug := builtins.LabelTransformerPlugin{
		Labels: map[string]string{
			"app.opendatahub.io/" + componentName: "true",
			"app.kubernetes.io/part-of":           componentName,
		},
		FieldSpecs: []types.FieldSpec{
			{
				Gvk:                resid.Gvk{Kind: "Deployment"},
				Path:               "spec/template/metadata/labels",
				CreateIfNotPresent: true,
			},
			{
				Gvk:                resid.Gvk{Kind: "Deployment"},
				Path:               "spec/selector/matchLabels",
				CreateIfNotPresent: true,
			},
			{
				Gvk:                resid.Gvk{},
				Path:               "metadata/labels",
				CreateIfNotPresent: true,
			},
		},
	}

	return nsplug.Transform(resMap)
}
