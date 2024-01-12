package envtestutil

import (
	"fmt"
	"os"
	"path/filepath"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

func FindProjectRoot() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(currentDir, "go.mod")); err == nil {
			return filepath.FromSlash(currentDir), nil
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}

		currentDir = parentDir
	}

	return "", fmt.Errorf("project root not found")
}

// NewOrigin creates an origin object with specified component and name.
func NewOrigin(component featurev1.OwnerType, name string) featurev1.Origin {
	return featurev1.Origin{
		Type: component,
		Name: name,
	}
}
