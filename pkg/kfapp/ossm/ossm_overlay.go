package ossm

import (
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
	"os"
	"path"
	"strings"
)

const (
	serviceMeshOverlay = "service-mesh"
)

type appOverlay struct {
	application *kfconfig.Application
	name,
	cachedPath string
}

// ForEachExistingOverlay applies custom logic for each application which has service mesh overlay present on its path.
func (o *OssmInstaller) forEachExistingOverlay(apply func(overlay *appOverlay) error) error {
	cachePathsPerRepo := make(map[string]string, len(o.KfConfig.Status.Caches))
	for _, cache := range o.KfConfig.Status.Caches {
		cachePathsPerRepo[cache.Name] = cache.LocalPath
	}

	var multiErr *multierror.Error
	for _, application := range o.KfConfig.Spec.Applications {
		overlayDir := path.Join(cachePathsPerRepo[application.KustomizeConfig.RepoRef.Name], application.KustomizeConfig.RepoRef.Path, "overlays", serviceMeshOverlay)
		info, err := os.Stat(overlayDir)
		if err == nil && info.IsDir() {
			multiErr = multierror.Append(multiErr, apply(&appOverlay{
				name:        serviceMeshOverlay,
				application: &application,
				cachedPath:  overlayDir,
			}))
		}
	}

	return multiErr.ErrorOrNil()
}

// addServiceMeshOverlays adds service mesh overlay to an application if it exists on a path.
// This way it will be executed by kustomize without a need of adding it explicitly when Ossm Plugin is in use.
func (o *OssmInstaller) addServiceMeshOverlays() error {
	return o.forEachExistingOverlay(func(overlay *appOverlay) error {
		return o.AddApplicationOverlay(overlay.application.Name, overlay.name)
	})
}

func (o *OssmInstaller) addOssmEnvFile(envVars ...string) error {
	if len(envVars)%2 != 0 {
		return errors.New("defined env vars should be passed as pairs")
	}

	return o.forEachExistingOverlay(func(overlay *appOverlay) error {

		var builder strings.Builder

		for i := 0; i < len(envVars)-1; i += 2 {
			key := envVars[i]
			value := envVars[i+1]
			builder.WriteString(fmt.Sprintf("%s=%s\n", key, value))
		}

		file, err := os.Create(path.Join(overlay.cachedPath, "ossm.env"))
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = file.WriteString(builder.String())

		return err
	})
}
