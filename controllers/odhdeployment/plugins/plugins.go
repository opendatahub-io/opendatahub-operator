package plugins

import (
	odhdeploymentv1 "github.com/opendatahub-io/opendatahub-operator/api/v1"
	"sigs.k8s.io/kustomize/api/builtins"
	"sigs.k8s.io/kustomize/api/filters/namespace"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
)

func ApplyNamespacePlugin(instance *odhdeploymentv1.OdhDeployment, resMap resmap.ResMap) error {
	nsplug := builtins.NamespaceTransformerPlugin{
		ObjectMeta: types.ObjectMeta{
			Name:      "odh-namespace-plugin",
			Namespace: instance.Namespace,
		},
		FieldSpecs: []types.FieldSpec{
			{
				Gvk:                resid.Gvk{},
				Path:               "metadata/namespace",
				CreateIfNotPresent: true,
			},
			{
				Gvk: resid.Gvk{
					Group: "rbac.authorization.k8s.io",
					Kind:  "ClusterRoleBinding",
				},
				Path:               "subjects/namespace",
				CreateIfNotPresent: true,
			},
			{
				Gvk: resid.Gvk{
					Group: "rbac.authorization.k8s.io",
					Kind:  "RoleBinding",
				},
				Path:               "subjects/namespace",
				CreateIfNotPresent: true,
			},
		},
		UnsetOnly:              false,
		SetRoleBindingSubjects: namespace.AllServiceAccountSubjects,
	}
	// Add namespace plugin
	err := nsplug.Transform(resMap)
	if err != nil {
		return err
	}
	return nil
}
