package plugins

import (
	"sigs.k8s.io/kustomize/api/builtins" //nolint:staticcheck // Remove after package update
	"sigs.k8s.io/kustomize/api/filters/namespace"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"
)

// CreateNamespaceApplierPlugin creates a plugin to ensure resources have the specified target namespace.
func CreateNamespaceApplierPlugin(targetNamespace string) *builtins.NamespaceTransformerPlugin {
	return &builtins.NamespaceTransformerPlugin{
		ObjectMeta: types.ObjectMeta{
			Name:      "odh-namespace-plugin",
			Namespace: targetNamespace,
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
			{
				Gvk: resid.Gvk{
					Group: "admissionregistration.k8s.io",
					Kind:  "ValidatingWebhookConfiguration",
				},
				Path:               "webhooks/clientConfig/service/namespace",
				CreateIfNotPresent: false,
			},
			{
				Gvk: resid.Gvk{
					Group: "admissionregistration.k8s.io",
					Kind:  "MutatingWebhookConfiguration",
				},
				Path:               "webhooks/clientConfig/service/namespace",
				CreateIfNotPresent: false,
			},
			{
				Gvk: resid.Gvk{
					Group: "apiextensions.k8s.io",
					Kind:  "CustomResourceDefinition",
				},
				Path:               "spec/conversion/webhook/clientConfig/service/namespace",
				CreateIfNotPresent: false,
			},
		},
		UnsetOnly:              false,
		SetRoleBindingSubjects: namespace.AllServiceAccountSubjects,
	}
}
