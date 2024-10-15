package plugins

import (
	"errors"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

var (
	AllowListedFields = []RemoverPlugin{
		// for resources, i.e cpu and memory
		{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "template", "spec", "containers", "*", "resources"},
		},
		// for replicas
		{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "replicas"},
		},
	}
)

// Removes the field from the resources of ResMap if they match GVK.
type RemoverPlugin struct {
	Gvk  schema.GroupVersionKind
	Path []string
}

var _ resmap.Transformer = &RemoverPlugin{}

// Transform removes the field from ResMap if they match filter.
func (p *RemoverPlugin) Transform(m resmap.ResMap) error {
	filter := RemoverFilter{
		Gvk:  p.Gvk,
		Path: p.Path,
	}
	return m.ApplyFilter(filter)
}

// TransformResource works only on one resource, not on the whole ResMap.
func (p *RemoverPlugin) TransformResource(r *resource.Resource) error {
	filter := RemoverFilter{
		Gvk:  p.Gvk,
		Path: p.Path,
	}

	nodes := []*kyaml.RNode{&r.RNode}
	_, err := filter.Filter(nodes)
	return err
}

type RemoverFilter struct {
	Gvk  schema.GroupVersionKind
	Path []string
}

var _ kio.Filter = RemoverFilter{}

func (f RemoverFilter) Filter(nodes []*kyaml.RNode) ([]*kyaml.RNode, error) {
	return kio.FilterAll(kyaml.FilterFunc(f.run)).Filter(nodes)
}

func (f RemoverFilter) run(node *kyaml.RNode) (*kyaml.RNode, error) {
	pathLen := len(f.Path)
	if pathLen == 0 {
		return node, errors.New("no field set to remove, path to the field cannot be empty")
	}

	return ClearFieldFor(node, f.Gvk, f.Path)
}

func ClearFieldFor(node *kyaml.RNode, gvk schema.GroupVersionKind, fieldPath []string) (*kyaml.RNode, error) {
	pathLen := len(fieldPath)
	if pathLen == 0 {
		return node, nil
	}

	typeMeta := kyaml.TypeMeta{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
	}

	meta, err := node.GetMeta()
	if err != nil {
		return node, err
	}

	if meta.TypeMeta != typeMeta {
		return node, nil
	}

	path := fieldPath[:pathLen-1]
	name := fieldPath[pathLen-1]

	matcher := &kyaml.PathMatcher{Path: path}
	result, err := node.Pipe(matcher)
	if err != nil {
		return node, err
	}

	return node, result.VisitElements(
		func(node *kyaml.RNode) error {
			return node.PipeE(kyaml.FieldClearer{Name: name})
		})
}

func ClearField(node *kyaml.RNode, fieldPath []string) (*kyaml.RNode, error) {
	pathLen := len(fieldPath)
	if pathLen == 0 {
		return node, nil
	}

	path := fieldPath[:pathLen-1]
	name := fieldPath[pathLen-1]

	matcher := &kyaml.PathMatcher{Path: path}
	result, err := node.Pipe(matcher)
	if err != nil {
		return node, err
	}

	return node, result.VisitElements(
		func(node *kyaml.RNode) error {
			return node.PipeE(kyaml.FieldClearer{Name: name})
		})
}
