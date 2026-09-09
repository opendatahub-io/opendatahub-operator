package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type RelatedImageException struct {
	Image string `yaml:"image"`
}

type RelatedImagesConfig struct {
	ODHExceptions   []RelatedImageException `yaml:"odh_exceptions"`
	RHOAIExceptions []RelatedImageException `yaml:"rhoai_exceptions"`
}

func LoadRelatedImagesConfig(path string) (*RelatedImagesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading related images config: %w", err)
	}

	var cfg RelatedImagesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing related images config: %w", err)
	}

	return &cfg, nil
}

func (c *RelatedImagesConfig) IsPlatformException(platform, envName string) bool {
	var exceptions []RelatedImageException
	switch platform {
	case "odh":
		exceptions = c.ODHExceptions
	case "rhoai":
		exceptions = c.RHOAIExceptions
	default:
		return false
	}

	for _, exception := range exceptions {
		if exception.Image == envName {
			return true
		}
	}
	return false
}
