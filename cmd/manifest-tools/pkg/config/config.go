package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type ManifestsConfig struct {
	Components      map[string]Component     `yaml:"components"`
	CCMCharts       map[string]Component     `yaml:"ccmCharts"`
	ComponentCharts map[string]Component     `yaml:"componentCharts"`
	ImageOverrides  map[string]ImageOverride `yaml:"imageOverrides"`
}

type Component struct {
	ODH   *PlatformRepo `yaml:"odh,omitempty"`
	RHOAI *PlatformRepo `yaml:"rhoai,omitempty"`
}

type PlatformRepo struct {
	Repo       string `yaml:"repo"`
	Ref        string `yaml:"ref"`
	SourcePath string `yaml:"sourcePath"`
}

type ImageOverride struct {
	Component    string         `yaml:"component"`
	ParamsEnvKey string         `yaml:"paramsEnvKey,omitempty"`
	TagTemplate  string         `yaml:"tagTemplate,omitempty"`
	Base         string         `yaml:"base,omitempty"`
	ODH          *PlatformImage `yaml:"odh,omitempty"`
	RHOAI        *PlatformImage `yaml:"rhoai,omitempty"`
}

type PlatformImage struct {
	Base    string `yaml:"base,omitempty"`
	Digest  string `yaml:"digest,omitempty"`
	Pinned  bool   `yaml:"pinned,omitempty"`
	SHAFrom string `yaml:"shaFrom,omitempty"`
}

var DigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (pi *PlatformImage) HasValidDigest() bool {
	return pi != nil && DigestPattern.MatchString(pi.Digest)
}

func (o *ImageOverride) PlatformImage(platform string) *PlatformImage {
	switch platform {
	case "odh":
		return o.ODH
	case "rhoai":
		return o.RHOAI
	default:
		return nil
	}
}

func (o *ImageOverride) SetPlatformImage(platform string, img *PlatformImage) {
	switch platform {
	case "odh":
		o.ODH = img
	case "rhoai":
		o.RHOAI = img
	}
}

func (c *Component) PlatformRepo(platform string) *PlatformRepo {
	switch platform {
	case "odh":
		return c.ODH
	case "rhoai":
		return c.RHOAI
	default:
		return nil
	}
}

func ExtractSHA(ref string) string {
	if idx := strings.LastIndex(ref, "@"); idx >= 0 {
		return ref[idx+1:]
	}
	return ""
}

func ExtractBranch(ref string) string {
	if idx := strings.LastIndex(ref, "@"); idx >= 0 {
		return ref[:idx]
	}
	return ref
}

func Load(path string) (*ManifestsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg ManifestsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

func (c *ManifestsConfig) FindComponent(name string) *Component {
	if comp, ok := c.Components[name]; ok {
		return &comp
	}
	if comp, ok := c.CCMCharts[name]; ok {
		return &comp
	}
	if comp, ok := c.ComponentCharts[name]; ok {
		return &comp
	}
	return nil
}

// NodeDoc loads the YAML as a yaml.Node tree for comment-preserving edits.
type NodeDoc struct {
	Root *yaml.Node
}

func LoadNode(path string) (*NodeDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &NodeDoc{Root: &doc}, nil
}

func (d *NodeDoc) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(d.Root); err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	return enc.Close()
}

// SetImageOverrideField sets a field (base or digest) on a specific platform's image override.
func (d *NodeDoc) SetImageOverrideField(envName, platform, field, value string) error {
	root := d.Root.Content[0] // document node → mapping node

	overridesNode := findMapValue(root, "imageOverrides")
	if overridesNode == nil {
		return fmt.Errorf("imageOverrides not found")
	}

	entryNode := findMapValue(overridesNode, envName)
	if entryNode == nil {
		return fmt.Errorf("imageOverrides.%s not found", envName)
	}

	platNode := findMapValue(entryNode, platform)
	if platNode == nil {
		platNode = &yaml.Node{Kind: yaml.MappingNode}
		entryNode.Content = append(entryNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: platform},
			platNode,
		)
	}

	setMapField(platNode, field, value)
	return nil
}

func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
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

func setMapField(mapping *yaml.Node, key, value string) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Style = yaml.DoubleQuotedStyle
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Style: yaml.DoubleQuotedStyle},
	)
}
