package plugins

import (
	"sigs.k8s.io/kustomize/api/builtins" //nolint:staticcheck //Remove after package update
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// CreateAddLabelsPlugin creates a label transformer plugin that ensures resources
// to which this plugin is applied will have the Open Data Hub common labels included.
//
// It has a following characteristics:
//   - It adds labels to the "metadata/labels" path for all resource kinds.
//   - It adds labels to the "spec/template/metadata/labels" and "spec/selector/matchLabels" paths
//     for resources of kind "Deployment".
func CreateAddLabelsPlugin(componentName string) *builtins.LabelTransformerPlugin {
	return &builtins.LabelTransformerPlugin{
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
}
