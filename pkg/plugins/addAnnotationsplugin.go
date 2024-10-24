package plugins

import (
	"sigs.k8s.io/kustomize/api/builtins" //nolint:staticcheck // Remove after package update
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
)

func CreateAddAnnotationsPlugin(annotations map[string]string) *builtins.AnnotationsTransformerPlugin {
	return &builtins.AnnotationsTransformerPlugin{
		Annotations: annotations,
		FieldSpecs: []types.FieldSpec{
			{
				Gvk:                resid.Gvk{},
				Path:               "metadata/annotations",
				CreateIfNotPresent: true,
			},
		},
	}
}
