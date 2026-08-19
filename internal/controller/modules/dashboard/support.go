package dashboard

import (
	"os"
	"sort"
	"strings"
)

const relatedImageModArchPrefix = "RELATED_IMAGE_ODH_MOD_ARCH_"

// coreRelatedImages lists RELATED_IMAGE_* env vars that are NOT module-arch
// images but are still needed by the dashboard-operator.
var coreRelatedImages = []string{
	"RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE",
	"RELATED_IMAGE_ODH_AUTOML_IMAGE",
	"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
	"RELATED_IMAGE_ODH_CORE_BFF_IMAGE",
	"RELATED_IMAGE_POSTGRESQL_16_IMAGE",
}

// relatedImages returns RELATED_IMAGE_* environment variables required by the
// dashboard-operator Deployment. Module-arch images (RELATED_IMAGE_ODH_MOD_ARCH_*)
// are discovered dynamically from the process environment so that new modules
// are forwarded automatically without code changes here.
func relatedImages() []string {
	result := make([]string, 0, len(coreRelatedImages)+8)
	result = append(result, coreRelatedImages...)

	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(name, relatedImageModArchPrefix) {
			result = append(result, name)
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
