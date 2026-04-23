package resources

import (
	"context"

	"github.com/k8s-manifest-kit/engine/pkg/pipeline"
	"github.com/k8s-manifest-kit/engine/pkg/postrenderer"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var defaultPostRenderers = []engineTypes.PostRenderer{
	postrenderer.ApplyOrder(),
}

// SortByApplyOrder reorders resources into dependency order for cluster
// application: foundational resources (Namespace, CRD, etc.) first,
// webhooks last.
func SortByApplyOrder(ctx context.Context, resources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return pipeline.ApplyPostRenderers(ctx, resources, defaultPostRenderers)
}
