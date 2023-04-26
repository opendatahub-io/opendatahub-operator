package odhdeployment

import (
	"context"
	"fmt"
	odhdeploymentv1 "github.com/opendatahub-io/opendatahub-operator/api/v1"
	"github.com/opendatahub-io/opendatahub-operator/controllers/odhdeployment/plugins"
	"os"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
)

const localManifestFolder = "/tmp/odh-manifests/"

func (r *OdhDeploymentReconciler) deployComponentManifest(ctx context.Context, instance *odhdeploymentv1.OdhDeployment, component odhdeploymentv1.Component) error {

	err := downloadManifests(component)
	if err != nil {
		r.Log.Info("Error during manifest download", "err", err)
		return err
	}
	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()

	// Create resmap
	// Use kustomization file under /tmp/odh-manifests/component/ or use `default` overlay
	var resMap resmap.ResMap
	_, err = os.Stat(localManifestFolder + component.Name + "/kustomization.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			resMap, err = k.Run(fs, localManifestFolder+component.Name+"/base")
		}
	} else {
		resMap, err = k.Run(fs, localManifestFolder+component.Name)
	}

	if err != nil {
		return fmt.Errorf("error during resmap resources: %v", err)
	}

	// Apply NamespaceTransformer Plugin
	if err := plugins.ApplyNamespacePlugin(instance, resMap); err != nil {
		return err
	}

	objs, err := getResources(resMap)
	if err != nil {
		return err
	}

	// Create or update resources in the cluster
	for _, obj := range objs {

		err = r.createOrUpdate(ctx, instance, obj)
		if err != nil {
			return err
		}
	}

	return nil
}
