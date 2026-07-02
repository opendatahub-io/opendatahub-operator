package dashboard

// relatedImages returns RELATED_IMAGE_* environment variables required by the
// dashboard-operator Deployment. Values are sourced from the former in-tree
// component handler imagesMap.
func relatedImages() []string {
	return []string{
		"RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MLFLOW_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_EVAL_HUB_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTORAG_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AGENT_OPS_IMAGE",
		"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
	}
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
