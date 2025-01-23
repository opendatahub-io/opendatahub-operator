package releases

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/operator-framework/api/pkg/lib/version"
	"gopkg.in/yaml.v3"
	"path/filepath"
)

const (
	ComponentMetadataFilename = "component_metadata.yaml"
)

// ComponentReleasesMeta represents the metadata for releases in the component_metadata.yaml file.
// It contains a list of releases associated with a specific component.
type ComponentReleasesMeta struct {
	Releases []ComponentReleaseMeta `yaml:"releases,omitempty"`
}

// ComponentReleaseMeta represents the metadata of a single release within the component_metadata.yaml file.
// It includes the release name, version, and repository URL.
type ComponentReleaseMeta struct {
	Name    string `yaml:"name,omitempty"`
	Version string `yaml:"version,omitempty"`
	RepoURL string `yaml:"repoUrl,omitempty"`
}

type Action struct {
	labels map[string]string
}

type ActionOpts func(*Action)

// run executes the reconciliation logic for fetching and processing component releases.
//
// This function:
// 1. Reads the metadata file for the specified component.
// 2. Parses the metadata file to extract release information.
// 3. Updates the component's release status in the reconciliation request.
//
// Parameters:
// - ctx: The context for managing deadlines and cancellations.
// - rr: The reconciliation request containing the component instance.
//
// Returns:
// - An error if the operation fails.
func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	// Ensure the resource implements the WithReleases interface
	obj, ok := rr.Instance.(common.WithReleases)
	if !ok {
		return fmt.Errorf("resource instance %v is not a WithReleases", rr.Instance)
	}

	// Build the path to the component metadata file
	controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
	metadataPath := filepath.Join(odhdeploy.DefaultManifestPath, controllerName, ComponentMetadataFilename)

	// Read the YAML file
	yamlData, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("metadata file not found at %s", metadataPath)
		}
		return fmt.Errorf("error reading metadata file: %w", err)
	}

	// Unmarshal YAML into defined struct
	var componentMeta ComponentReleasesMeta
	if err := yaml.Unmarshal(yamlData, &componentMeta); err != nil {
		return fmt.Errorf("error unmarshaling YAML: %w", err)
	}

	// Parse and populate releases
	componentReleasesStatus := make([]common.ComponentReleaseStatus, 0, len(componentMeta.Releases))
	for _, release := range componentMeta.Releases {
		//componentVersion, err := semver.Parse(release.Version)
		componentVersion, err := semver.Parse(strings.Trim(release.Version, "v"))
		if err != nil {
			return fmt.Errorf("invalid version format for release %s: %w", release.Name, err)
		}
		componentReleasesStatus = append(componentReleasesStatus, common.ComponentReleaseStatus{
			Name:    release.Name,
			Version: version.OperatorVersion{Version: componentVersion},
			RepoURL: release.RepoURL,
		})
	}

	// Update the release status in the resource
	*obj.GetReleaseStatus() = componentReleasesStatus

	return nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
