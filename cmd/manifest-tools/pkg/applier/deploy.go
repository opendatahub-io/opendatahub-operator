package applier

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type DeployOptions struct {
	ConfigFile  string
	Platform    string
	ManagerFile string
}

func ApplyDeploy(opts DeployOptions) error {
	overrides, err := loadOverridesFromConfig(opts.ConfigFile, opts.Platform)
	if err != nil {
		return err
	}

	if len(overrides) == 0 {
		slog.Info("No overrides to apply")
		return nil
	}

	data, err := os.ReadFile(opts.ManagerFile)
	if err != nil {
		return fmt.Errorf("reading manager file: %w", err)
	}

	docs, err := parseMultiDoc(data)
	if err != nil {
		return fmt.Errorf("parsing manager file: %w", err)
	}

	deployIdx := findDeploymentDoc(docs)
	if deployIdx < 0 {
		return fmt.Errorf("no Deployment document found in %s", opts.ManagerFile)
	}

	root := docs[deployIdx].Content[0]

	envNode, err := findManagerEnvNode(root)
	if err != nil {
		return fmt.Errorf("finding env node: %w", err)
	}

	changes := 0
	for _, ov := range overrides {
		updated := updateEnvVar(envNode, ov.Name, ov.Value)
		if updated {
			slog.Info("Updated", slog.String("env", ov.Name), slog.String("value", ov.Value))
		} else {
			appendEnvVar(envNode, ov.Name, ov.Value)
			slog.Info("Added", slog.String("env", ov.Name), slog.String("value", ov.Value))
		}
		changes++
	}

	if changes == 0 {
		slog.Info("No changes needed to manager.yaml")
		return nil
	}

	if err := writeMultiDoc(opts.ManagerFile, docs); err != nil {
		return fmt.Errorf("writing manager file: %w", err)
	}

	slog.Info("Updated manager.yaml", slog.Int("changes", changes))
	return nil
}

func parseMultiDoc(data []byte) ([]*yaml.Node, error) {
	var docs []*yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		docs = append(docs, &doc)
	}
	return docs, nil
}

func findDeploymentDoc(docs []*yaml.Node) int {
	for i, doc := range docs {
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		kind := findScalarValue(root, "kind")
		if kind == "Deployment" {
			return i
		}
	}
	return -1
}

func findScalarValue(mapping *yaml.Node, key string) string {
	if mapping.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1].Value
		}
	}
	return ""
}

func findManagerEnvNode(root *yaml.Node) (*yaml.Node, error) {
	spec := findMapNode(root, "spec")
	if spec == nil {
		return nil, fmt.Errorf("spec not found")
	}
	template := findMapNode(spec, "template")
	if template == nil {
		return nil, fmt.Errorf("spec.template not found")
	}
	podSpec := findMapNode(template, "spec")
	if podSpec == nil {
		return nil, fmt.Errorf("spec.template.spec not found")
	}
	containers := findSequenceNode(podSpec, "containers")
	if containers == nil || len(containers.Content) == 0 {
		return nil, fmt.Errorf("spec.template.spec.containers not found")
	}

	for _, container := range containers.Content {
		name := findScalarValue(container, "name")
		if name == "manager" {
			env := findSequenceNode(container, "env")
			if env == nil {
				env = &yaml.Node{Kind: yaml.SequenceNode}
				container.Content = append(container.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "env"},
					env,
				)
			}
			return env, nil
		}
	}

	return nil, fmt.Errorf("container 'manager' not found")
}

func findMapNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func findSequenceNode(mapping *yaml.Node, key string) *yaml.Node {
	node := findMapNode(mapping, key)
	if node != nil && node.Kind == yaml.SequenceNode {
		return node
	}
	return nil
}

func updateEnvVar(envNode *yaml.Node, name, value string) bool {
	for _, entry := range envNode.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		entryName := findScalarValue(entry, "name")
		if entryName != name {
			continue
		}

		// Check if entry uses valueFrom (not a plain value) — skip
		if findMapNode(entry, "valueFrom") != nil {
			continue
		}

		for i := 0; i < len(entry.Content)-1; i += 2 {
			if entry.Content[i].Value == "value" {
				entry.Content[i+1].Value = value
				entry.Content[i+1].Tag = "!!str"
				return true
			}
		}
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "value"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
		)
		return true
	}
	return false
}

func appendEnvVar(envNode *yaml.Node, name, value string) {
	entry := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name"},
			{Kind: yaml.ScalarNode, Value: name},
			{Kind: yaml.ScalarNode, Value: "value"},
			{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
		},
	}
	envNode.Content = append(envNode.Content, entry)
}

func writeMultiDoc(path string, docs []*yaml.Node) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return err
		}
	}
	return enc.Close()
}

func loadOverridesFromConfig(configFile, platform string) ([]envVar, error) {
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, err
	}

	if platform == "OpenDataHub" {
		platform = "odh"
	} else if platform != "odh" && platform != "rhoai" {
		platform = "rhoai"
	}

	var vars []envVar
	for envName, override := range cfg.ImageOverrides {
		if !strings.HasPrefix(envName, "RELATED_IMAGE_") {
			continue
		}

		pi := override.PlatformImage(platform)
		if pi == nil || pi.Digest == "" || pi.Base == "" {
			continue
		}

		if !config.DigestPattern.MatchString(pi.Digest) {
			slog.Warn("Invalid digest, skipping", slog.String("env", envName), slog.String("digest", pi.Digest))
			continue
		}

		vars = append(vars, envVar{
			Name:  envName,
			Value: fmt.Sprintf("%s@%s", pi.Base, pi.Digest),
		})
	}

	return vars, nil
}
