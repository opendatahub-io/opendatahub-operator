package kustomize

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

func NodeToUnstructured(n *kyaml.RNode) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion(n.GetApiVersion())
	u.SetKind(n.GetKind())
	u.SetNamespace(n.GetNamespace())
	u.SetName(n.GetName())

	return u
}
