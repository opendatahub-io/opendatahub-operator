package releases

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ComponentMetadataFilename = "component_metadata.yaml"
)

type Action struct {
	metadataFilePath       string
	componentReleaseStatus []common.ComponentRelease
}

// WithMetadataFilePath is an ActionOpts function that sets a custom metadata file path.
func WithMetadataFilePath(filePath string) ActionOpts {
	return func(a *Action) {
		a.metadataFilePath = filePath
	}
}

// WithComponentReleaseStatus allows setting a custom ComponentRelease for the action.
// This is mostly for testing purposes to simulate cached or pre-existing release statuses.
// In production scenarios, the action should fetch the current release status from the source rather than being manually set.
func WithComponentReleaseStatus(status []common.ComponentRelease) ActionOpts {
	return func(a *Action) {
		a.componentReleaseStatus = status
	}
}

type ActionOpts func(*Action)

// run is responsible for executing the logic of reconciling and processing component releases.
//
// This function performs the following:
// 1. Verifies that the resource instance implements the `WithReleases` interface.
// 2. If the release status is not already cached, it calls the `render` method to fetch the releases from the metadata file.
// 3. Updates the release status on the resource instance with the processed release information.
//
// Parameters:
// - ctx: The context for managing deadlines and cancellations during the reconciliation process.
// - rr: The `ReconciliationRequest` containing the resource instance that needs to be reconciled.
//
// Returns:
//   - An error if the reconciliation fails at any step. This could occur if the resource doesn't implement the required interface
//     or if the metadata file cannot be read or processed.
func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	// Ensure the resource implements the WithReleases interface
	obj, ok := rr.Instance.(common.WithReleases)
	if !ok {
		return fmt.Errorf("resource instance %v is not a WithReleases", rr.Instance)
	}

	// If the release status is empty, or if the DevFlags.Manifests is set, render the release information.
	// This ensures that releases are either reprocessed or fetched from the manifests specified in DevFlags.
	if len(a.componentReleaseStatus) == 0 || resources.InstanceHasDevFlags(rr.Instance) {
		releases, err := a.render(ctx, rr)
		if err != nil {
			return err
		}
		a.componentReleaseStatus = releases
	}

	// Update the release status in the resource
	obj.SetReleaseStatus(a.componentReleaseStatus)

	return nil
}

// render reads and processes the component releases from the metadata file.
//
// This function performs the following:
// 1. Reads the component metadata YAML file (either from a custom or default path).
// 2. Parses the YAML file and extracts the release metadata (name, version, repo URL).
// 3. Returns a slice of `ComponentRelease` containing the processed release information.
//
// Parameters:
// - rr: The `ReconciliationRequest` containing the resource instance. This is used to determine the metadata file path.
//
// Returns:
// - A slice of `common.ComponentRelease`, representing the parsed release information from the metadata file.
// - An error if there is an issue with reading the file, unmarshalling the YAML, or processing the release data.
func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) ([]common.ComponentRelease, error) {
	log := logf.FromContext(ctx)

	// Determine the metadata file path
	var metadataPath string
	if a.metadataFilePath != "" {
		metadataPath = a.metadataFilePath
	} else {
		// Build the path to the component metadata file
		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		metadataPath = filepath.Join(odhdeploy.DefaultManifestPath, controllerName, ComponentMetadataFilename)
	}

	// Read the YAML file
	yamlData, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Log a message indicating the file doesn't exist but do not return an error
			// Log this as a warning, as it's not necessarily a failure if the file is absent
			log.V(3).Info("Metadata file not found, proceeding with empty releases", "metadataFilePath", metadataPath)
			// Return an empty slice of releases instead of an error
			return nil, nil
		}
		return nil, fmt.Errorf("error reading metadata file: %w", err)
	}

	// Unmarshal YAML into defined struct
	var componentMeta common.ComponentReleaseStatus
	if err := yaml.Unmarshal(yamlData, &componentMeta); err != nil {
		return nil, fmt.Errorf("error unmarshaling YAML: %w", err)
	}

	// Parse and populate releases
	componentReleasesStatus := make([]common.ComponentRelease, 0, len(componentMeta.Releases))
	for _, release := range componentMeta.Releases {
		componentVersion := strings.TrimSpace(release.Version)

		// Appending the component version only if it's not empty
		if componentVersion != "" {
			componentReleasesStatus = append(componentReleasesStatus, common.ComponentRelease{
				Name:    release.Name,
				Version: componentVersion,
				RepoURL: release.RepoURL,
			})
		}
	}

	return componentReleasesStatus, nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
