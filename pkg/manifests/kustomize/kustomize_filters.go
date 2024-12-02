package kustomize

import (
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

var _ resmap.Transformer = &filterPlugin{}
var _ kio.Filter = &filterProxy{}

type filterPlugin struct {
	f FilterFn
}

func (p *filterPlugin) Transform(m resmap.ResMap) error {
	return m.ApplyFilter(&filterProxy{
		f: p.f,
	})
}

type filterProxy struct {
	f FilterFn
}

func (f *filterProxy) Filter(nodes []*kyaml.RNode) ([]*kyaml.RNode, error) {
	return f.f(nodes)
}
