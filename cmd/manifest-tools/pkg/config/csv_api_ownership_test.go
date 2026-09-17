package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestCSVPlatformOwnershipAndInternalObjects(t *testing.T) {
	for _, csvPath := range []string{
		"config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml",
		"config/rhoai/manifests/bases/rhods-operator.clusterserviceversion.yaml",
	} {
		t.Run(csvPath, func(t *testing.T) {
			csvBytes, err := os.ReadFile(filepath.Join(repoRoot(t), csvPath))
			require.NoError(t, err)

			var csv struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
				Spec struct {
					CustomResourceDefinitions struct {
						Owned []struct {
							Name    string `json:"name"`
							Version string `json:"version"`
						} `json:"owned"`
					} `json:"customresourcedefinitions"`
				} `json:"spec"`
			}
			require.NoError(t, yaml.Unmarshal(csvBytes, &csv))

			ownedVersions := make([]string, 0, 2)
			ownedNames := make([]string, 0, len(csv.Spec.CustomResourceDefinitions.Owned))
			for _, owned := range csv.Spec.CustomResourceDefinitions.Owned {
				ownedNames = append(ownedNames, owned.Name)
				if owned.Name == "platforms.config.opendatahub.io" {
					ownedVersions = append(ownedVersions, owned.Version)
				}
			}
			require.ElementsMatch(t, []string{"v1alpha1", "v1alpha2"}, ownedVersions,
				"Platform must be CSV-owned at both served versions")
			require.Contains(t, ownedNames, "trustyais.components.platform.opendatahub.io",
				"TrustyAI ownership is unchanged by the Platform API change")

			const internalObjectsAnnotation = "operators.operatorframework.io/internal-objects"
			var internalObjects []string
			require.NoError(t, json.Unmarshal(
				[]byte(csv.Metadata.Annotations[internalObjectsAnnotation]), &internalObjects))
			require.Contains(t, internalObjects, "platforms.config.opendatahub.io")
			require.Contains(t, internalObjects, "kueues.components.platform.opendatahub.io")
			require.Contains(t, internalObjects, "featuretrackers.features.opendatahub.io")
			require.Contains(t, internalObjects, "trustyais.components.platform.opendatahub.io")
		})
	}
}
