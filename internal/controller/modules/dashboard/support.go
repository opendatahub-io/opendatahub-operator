package dashboard

import (
	"os"
	"sort"
	"strings"
)

// coreRelatedImages lists RELATED_IMAGE_* env vars for core dashboard
// infrastructure that is always deployed regardless of which modules are enabled.
var coreRelatedImages = []string{
	"RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	"RELATED_IMAGE_ODH_CORE_BFF_IMAGE",
	"RELATED_IMAGE_POSTGRESQL_16_IMAGE",
}

// moduleImagePrefixes lists env var prefixes for module-specific images.
// RELATED_IMAGE_ODH_MOD_ARCH_* covers module UI sidecar images. The remaining
// prefixes cover auxiliary module images (pipeline runtimes, job images) that
// predate the MOD_ARCH naming convention.
var moduleImagePrefixes = []string{
	"RELATED_IMAGE_ODH_MOD_ARCH_",
	"RELATED_IMAGE_ODH_AUTOML_",
	"RELATED_IMAGE_ODH_AUTORAG_",
	"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_",
}

// relatedImages returns RELATED_IMAGE_* environment variables required by the
// dashboard-operator Deployment. Core images are always included. Module images
// are discovered dynamically from the process environment by prefix matching so
// that new modules are forwarded automatically without code changes here.
func relatedImages() []string {
	result := make([]string, 0, len(coreRelatedImages)+8)
	result = append(result, coreRelatedImages...)

	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		for _, prefix := range moduleImagePrefixes {
			if strings.HasPrefix(name, prefix) {
				result = append(result, name)
				break
			}
		}
	}

	sort.Strings(result)
	return result
}

// emptyRelatedImageValues returns a Helm values map that overrides the chart's
// default relatedImages (which carry :main tags) with empty strings. Empty
// env vars are skipped by the dashboard-operator's resolveImageParams, so the
// digest-pinned defaults in params.env are preserved (odh-dashboard#8330).
func emptyRelatedImageValues() map[string]any {
	imgs := relatedImages()
	m := make(map[string]any, len(imgs))
	for _, name := range imgs {
		m[name] = ""
	}
	return m
}
