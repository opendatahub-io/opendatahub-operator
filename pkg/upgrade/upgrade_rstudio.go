package upgrade

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	imagev1 "github.com/openshift/api/image/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	imageTagOutdatedAnnotation          = "opendatahub.io/image-tag-outdated"
	workbenchImageRecommendedAnnotation = "opendatahub.io/workbench-image-recommended"
)

var (
	deprecatedRStudioBuildConfigs = []string{
		"rstudio-server-rhel9",
		"cuda-rstudio-server-rhel9",
	}
	deprecatedRStudioBuildImageStreams = []string{
		"rstudio-rhel9",
		"cuda-rstudio-rhel9",
	}
	deprecatedRStudioNotebookImageStreams = []string{
		"rstudio-notebook",
		"rstudio-gpu-notebook",
	}
	rstudioBuildConfigGVK = schema.GroupVersionKind{
		Group:   "build.openshift.io",
		Version: "v1",
		Kind:    "BuildConfig",
	}
)

// cleanupDeprecatedRStudioResources removes orphaned RStudio BuildConfigs and build
// ImageStreams left over from RHOAI 3.4, and marks remaining RStudio workbench
// ImageStreams as deprecated so they are hidden from the spawner UI.
// RStudio was removed from the notebooks repository in RHAIENG-4776.
//
// TODO: Remove this cleanup once upgrade from RHOAI 3.4 is no longer supported.
func cleanupDeprecatedRStudioResources(ctx context.Context, cli client.Client, applicationNS string) error {
	log := logf.FromContext(ctx)
	var multiErr *multierror.Error

	for _, name := range deprecatedRStudioBuildConfigs {
		if err := deleteRStudioBuildConfig(ctx, cli, applicationNS, name); err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}

	for _, name := range deprecatedRStudioBuildImageStreams {
		if err := deleteRStudioImageStream(ctx, cli, applicationNS, name); err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}

	for _, name := range deprecatedRStudioNotebookImageStreams {
		if err := deprecateRStudioImageStream(ctx, cli, applicationNS, name); err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}

	if multiErr != nil {
		return multiErr
	}

	log.Info("Completed RStudio deprecation cleanup", "namespace", applicationNS)
	return nil
}

func deleteRStudioBuildConfig(ctx context.Context, cli client.Client, namespace, name string) error {
	log := logf.FromContext(ctx)

	bc := &unstructured.Unstructured{}
	bc.SetGroupVersionKind(rstudioBuildConfigGVK)
	bc.SetName(name)
	bc.SetNamespace(namespace)

	if err := cli.Delete(ctx, bc); err != nil {
		if isIgnorableClusterResourceError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete BuildConfig %s: %w", name, err)
	}

	log.Info("Deleted deprecated RStudio BuildConfig", "name", name, "namespace", namespace)
	return nil
}

func isIgnorableClusterResourceError(err error) bool {
	return k8serr.IsNotFound(err) ||
		meta.IsNoMatchError(err) ||
		containsNoKindRegistered(err)
}

func containsNoKindRegistered(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no kind is registered")
}

func deleteRStudioImageStream(ctx context.Context, cli client.Client, namespace, name string) error {
	log := logf.FromContext(ctx)

	is := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := cli.Delete(ctx, is); err != nil {
		if isIgnorableClusterResourceError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete ImageStream %s: %w", name, err)
	}

	log.Info("Deleted deprecated RStudio build ImageStream", "name", name, "namespace", namespace)
	return nil
}

func deprecateRStudioImageStream(ctx context.Context, cli client.Client, namespace, name string) error {
	log := logf.FromContext(ctx)
	key := client.ObjectKey{Name: name, Namespace: namespace}
	updated := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		is := &imagev1.ImageStream{}
		if err := cli.Get(ctx, key, is); err != nil {
			if isIgnorableClusterResourceError(err) {
				return nil
			}
			return err
		}

		changed := false
		for i := range is.Spec.Tags {
			tag := &is.Spec.Tags[i]
			if tag.Annotations == nil {
				tag.Annotations = map[string]string{}
			}
			if tag.Annotations[imageTagOutdatedAnnotation] != "true" {
				tag.Annotations[imageTagOutdatedAnnotation] = "true"
				changed = true
			}
			if tag.Annotations[workbenchImageRecommendedAnnotation] != "false" {
				tag.Annotations[workbenchImageRecommendedAnnotation] = "false"
				changed = true
			}
		}
		if !changed {
			return nil
		}
		updated = true
		return cli.Update(ctx, is)
	})
	if err != nil {
		return fmt.Errorf("failed to deprecate ImageStream %s: %w", name, err)
	}
	if !updated {
		log.Info("RStudio workbench ImageStream already deprecated", "name", name, "namespace", namespace)
		return nil
	}

	log.Info("Deprecated RStudio workbench ImageStream", "name", name, "namespace", namespace)
	return nil
}
