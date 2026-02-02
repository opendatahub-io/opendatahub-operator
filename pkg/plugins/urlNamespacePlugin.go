package plugins

import (
	"strings"

	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// DefaultSourceNamespace is the default namespace placeholder used in manifests
// that should be replaced with the actual target namespace.
const DefaultSourceNamespace = "opendatahub"

// URLNamespaceTransformerPlugin transforms URLs in resources that contain
// namespace references in the format ".<namespace>.svc.cluster.local".
// This is useful when manifests contain hardcoded namespace references in URLs
// that need to be updated to match the deployment namespace.
type URLNamespaceTransformerPlugin struct {
	// SourceNamespace is the namespace to replace in URLs.
	// If empty, DefaultSourceNamespace ("opendatahub") is used.
	SourceNamespace string

	// TargetNamespace is the namespace to use in the transformed URLs.
	TargetNamespace string
}

var _ resmap.Transformer = &URLNamespaceTransformerPlugin{}

// Transform applies the URL namespace transformation to all resources in the ResMap.
func (p *URLNamespaceTransformerPlugin) Transform(m resmap.ResMap) error {
	sourceNs := p.SourceNamespace
	if sourceNs == "" {
		sourceNs = DefaultSourceNamespace
	}

	if p.TargetNamespace == "" || sourceNs == p.TargetNamespace {
		return nil
	}

	filter := &urlNamespaceFilter{
		sourceNamespace: sourceNs,
		targetNamespace: p.TargetNamespace,
	}

	return m.ApplyFilter(filter)
}

// TransformResource applies the URL namespace transformation to a single resource.
func (p *URLNamespaceTransformerPlugin) TransformResource(r *resource.Resource) error {
	sourceNs := p.SourceNamespace
	if sourceNs == "" {
		sourceNs = DefaultSourceNamespace
	}

	if p.TargetNamespace == "" || sourceNs == p.TargetNamespace {
		// Nothing to transform
		return nil
	}

	filter := &urlNamespaceFilter{
		sourceNamespace: sourceNs,
		targetNamespace: p.TargetNamespace,
	}

	nodes := []*kyaml.RNode{&r.RNode}
	_, err := filter.Filter(nodes)
	return err
}

// urlNamespaceFilter is a kio.Filter that transforms URLs containing namespace patterns.
type urlNamespaceFilter struct {
	sourceNamespace string
	targetNamespace string
}

var _ kio.Filter = &urlNamespaceFilter{}

// Filter applies the URL namespace transformation to all nodes.
func (f *urlNamespaceFilter) Filter(nodes []*kyaml.RNode) ([]*kyaml.RNode, error) {
	return kio.FilterAll(kyaml.FilterFunc(f.transformNode)).Filter(nodes)
}

// transformNode recursively transforms all string values in a node.
func (f *urlNamespaceFilter) transformNode(node *kyaml.RNode) (*kyaml.RNode, error) {
	return node, node.VisitFields(func(field *kyaml.MapNode) error {
		return f.visitValue(field.Value)
	})
}

// visitValue recursively visits and transforms values.
func (f *urlNamespaceFilter) visitValue(node *kyaml.RNode) error {
	if node == nil {
		return nil
	}

	switch node.YNode().Kind {
	case kyaml.ScalarNode:
		// Transform scalar (string) values
		f.transformScalar(node)
	case kyaml.MappingNode:
		// Recursively visit map entries
		return node.VisitFields(func(field *kyaml.MapNode) error {
			return f.visitValue(field.Value)
		})
	case kyaml.SequenceNode:
		// Recursively visit sequence elements
		elements, err := node.Elements()
		if err != nil {
			return err
		}
		for _, elem := range elements {
			if err := f.visitValue(elem); err != nil {
				return err
			}
		}
	}

	return nil
}

// transformScalar transforms a scalar node if it contains a URL with namespace pattern.
func (f *urlNamespaceFilter) transformScalar(node *kyaml.RNode) {
	value := node.YNode().Value
	if value == "" {
		return
	}

	// Replace patterns like .opendatahub.svc.cluster.local with .targetNamespace.svc.cluster.local
	sourcePattern := "." + f.sourceNamespace + ".svc.cluster.local"
	targetPattern := "." + f.targetNamespace + ".svc.cluster.local"

	if strings.Contains(value, sourcePattern) {
		newValue := strings.ReplaceAll(value, sourcePattern, targetPattern)
		node.YNode().Value = newValue
	}
}

// CreateURLNamespaceTransformerPlugin creates a plugin that transforms URLs containing
// namespace patterns from a source namespace to a target namespace.
//
// This is useful when manifests contain hardcoded URLs like:
//
//	http://service.opendatahub.svc.cluster.local:8080/api
//
// And need to be transformed to:
//
//	http://service.redhat-ods-applications.svc.cluster.local:8080/api
func CreateURLNamespaceTransformerPlugin(targetNamespace string) *URLNamespaceTransformerPlugin {
	return &URLNamespaceTransformerPlugin{
		SourceNamespace: DefaultSourceNamespace,
		TargetNamespace: targetNamespace,
	}
}

// CreateURLNamespaceTransformerPluginWithSource creates a plugin that transforms URLs
// from a custom source namespace to a target namespace.
func CreateURLNamespaceTransformerPluginWithSource(sourceNamespace, targetNamespace string) *URLNamespaceTransformerPlugin {
	return &URLNamespaceTransformerPlugin{
		SourceNamespace: sourceNamespace,
		TargetNamespace: targetNamespace,
	}
}
