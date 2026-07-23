package workbenches

// operandRelatedImages are RELATED_IMAGE_* env vars forwarded to the
// workbenches-operator manager container for operand image resolution.
func operandRelatedImages() []string {
	return relatedImages
}

// emptyRelatedImageValues returns a Helm values map that pre-declares operand
// RELATED_IMAGE_* keys with empty strings. Empty values are skipped by the
// chart template so workbenches-operator falls back to digest-pinned defaults
// in its bundled params.env when the platform operator has not injected the
// env var yet.
func emptyRelatedImageValues() map[string]any {
	imgs := operandRelatedImages()
	m := make(map[string]any, len(imgs))
	for _, name := range imgs {
		m[name] = ""
	}
	return m
}
