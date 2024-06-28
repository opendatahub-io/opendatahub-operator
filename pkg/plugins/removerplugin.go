package plugins

import (
	"errors"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// Removes the field from the resources of ResMap if they match GVK.
type RemoverPlugin struct {
	Gvk  schema.GroupVersionKind
	Path []string
}

var _ resmap.Transformer = &RemoverPlugin{}

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

	typeMeta := kyaml.TypeMeta{
		APIVersion: f.Gvk.GroupVersion().String(),
		Kind:       f.Gvk.Kind,
	}

	meta, err := node.GetMeta()
	if err != nil {
		return node, err
	}

	if meta.TypeMeta != typeMeta {
		return node, nil
	}

	path := f.Path[:pathLen-1]
	name := f.Path[pathLen-1]

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

func (p *RemoverPlugin) Transform(m resmap.ResMap) error {
	filter := RemoverFilter{
		Gvk:  p.Gvk,
		Path: p.Path,
	}
	return m.ApplyFilter(filter)
}

// TransformResource is an additional method to work not on the whole ResMap but one resource.
func (p *RemoverPlugin) TransformResource(r *resource.Resource) error {
	filter := RemoverFilter{
		Gvk:  p.Gvk,
		Path: p.Path,
	}

	nodes := []*kyaml.RNode{&r.RNode}
	_, err := filter.Filter(nodes)
	return err
}
