package plugins

import (
	"sigs.k8s.io/kustomize/api/builtins" //nolint:staticcheck //Remove after package update
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func ApplyAddLabelsPlugin(componentName string, resMap resmap.ResMap) error {
	nsplug := builtins.LabelTransformerPlugin{
		Labels: map[string]string{
			labels.ODH.Component(componentName): "true",
			labels.K8SCommon.PartOf:             componentName,
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
