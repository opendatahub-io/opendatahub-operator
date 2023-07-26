package ossm

import (
	"fmt"
	kftypesv3 "github.com/opendatahub-io/opendatahub-operator/apis/apps"
	configtypes "github.com/opendatahub-io/opendatahub-operator/apis/config"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/ossmplugin"
	"github.com/opendatahub-io/opendatahub-operator/pkg/secret"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type applier func(config *rest.Config, filename string, elems ...configtypes.NameValue) error

func (o *OssmInstaller) applyManifests() error {
	var apply applier

	for _, m := range o.manifests {
		if m.patch {
			apply = func(config *rest.Config, filename string, elems ...configtypes.NameValue) error {
				log.Info("patching using manifest", "name", m.name, "path", m.targetPath())
				return o.PatchResourceFromFile(filename, elems...)
			}
		} else {
			apply = func(config *rest.Config, filename string, elems ...configtypes.NameValue) error {
				log.Info("applying manifest", "name", m.name, "path", m.targetPath())
				return o.CreateResourceFromFile(filename, elems...)
			}
		}

		err := apply(
			o.config,
			m.targetPath(),
		)
		if err != nil {
			log.Error(err, "failed to create resource", "name", m.name, "path", m.targetPath())
			return err
		}
	}

	return nil
}

func (o *OssmInstaller) processManifests() error {
	if err := o.SyncCache(); err != nil {
		return internalError(err)
	}

	// TODO warn when file is not present instead of throwing an error
	// IMPORTANT: Order of locations from where we load manifests/templates to process is significant
	err := o.loadManifestsFrom(
		path.Join("control-plane", "base"),
		path.Join("control-plane", "filters"),
		path.Join("control-plane", "oauth"),
		path.Join("control-plane", "smm.tmpl"),
		path.Join("control-plane", "namespace.patch.tmpl"),

		path.Join("authorino", "namespace.tmpl"),
		path.Join("authorino", "smm.tmpl"),
		path.Join("authorino", "base"),
		path.Join("authorino", "rbac"),
		path.Join("authorino", "mesh-authz-ext-provider.patch.tmpl"),
	)
	if err != nil {
		return internalError(errors.WithStack(err))
	}

	data, err := o.prepareTemplateData()
	if err != nil {
		return internalError(errors.WithStack(err))
	}

	for i, m := range o.manifests {
		if err := m.processTemplate(data); err != nil {
			return internalError(errors.WithStack(err))
		}

		fmt.Printf("%d: %+v\n", i, m)
	}

	return nil
}

func (o *OssmInstaller) loadManifestsFrom(paths ...string) error {
	manifestRepo, ok := o.GetRepoCache(kftypesv3.ManifestsRepoName)
	if !ok {
		return internalError(errors.New("manifests repo is not defined."))
	}

	var err error
	var manifests []manifest
	for i := range paths {
		manifests, err = loadManifestsFrom(manifests, path.Join(manifestRepo.LocalPath, TMPL_LOCAL_PATH, paths[i]))
		if err != nil {
			return internalError(errors.WithStack(err))
		}
	}

	o.manifests = manifests

	return nil
}

func loadManifestsFrom(manifests []manifest, dir string) ([]manifest, error) {

	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		basePath := filepath.Base(path)
		manifests = append(manifests, manifest{
			name:     basePath,
			path:     path,
			patch:    strings.Contains(basePath, ".patch"),
			template: filepath.Ext(path) == ".tmpl",
		})
		return nil
	}); err != nil {
		return nil, internalError(errors.WithStack(err))
	}

	return manifests, nil
}

// TODO(smell) this is now holding two responsibilities:
// - creates data structure to be fed to templates
// - creates secrets using k8s API calls
func (o *OssmInstaller) prepareTemplateData() (interface{}, error) {
	data := struct {
		*ossmplugin.OssmPluginSpec
		OAuth oAuth
		Domain,
		AppNamespace string
	}{
		AppNamespace: o.KfConfig.Namespace,
	}

	spec, err := o.GetPluginSpec()
	if err != nil {
		return nil, internalError(errors.WithStack(err))
	}
	data.OssmPluginSpec = spec

	if domain, err := GetDomain(o.config); err == nil {
		data.Domain = domain
	} else {
		return nil, internalError(errors.WithStack(err))
	}

	var clientSecret, hmac *secret.Secret
	if clientSecret, err = secret.NewSecret("ossm-odh-oauth", "random", 32); err != nil {
		return nil, internalError(errors.WithStack(err))
	}

	if hmac, err = secret.NewSecret("ossm-odh-hmac", "random", 32); err != nil {
		return nil, internalError(errors.WithStack(err))
	}

	if oauthServerDetailsJson, err := GetOAuthServerDetails(); err == nil {
		hostName, port, errUrlParsing := ExtractHostNameAndPort(oauthServerDetailsJson.Get("issuer").MustString("issuer"))
		if errUrlParsing != nil {
			return nil, internalError(errUrlParsing)
		}

		data.OAuth = oAuth{
			AuthzEndpoint: oauthServerDetailsJson.Get("authorization_endpoint").MustString("authorization_endpoint"),
			TokenEndpoint: oauthServerDetailsJson.Get("token_endpoint").MustString("token_endpoint"),
			Route:         hostName,
			Port:          port,
			ClientSecret:  clientSecret.Value,
			Hmac:          hmac.Value,
		}
	} else {
		return nil, internalError(errors.WithStack(err))
	}

	if spec.Mesh.Certificate.Generate {
		if err := o.createSelfSignedCerts(data.Domain, metav1.ObjectMeta{
			Name:      spec.Mesh.Certificate.Name,
			Namespace: spec.Mesh.Namespace,
		}); err != nil {
			return nil, internalError(err)
		}
	}

	if err := o.createEnvoySecret(data.OAuth, metav1.ObjectMeta{
		Name:      data.AppNamespace + "-oauth2-tokens",
		Namespace: data.Mesh.Namespace,
	}); err != nil {
		return nil, internalError(err)
	}

	return data, nil
}
