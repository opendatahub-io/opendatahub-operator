package template

import (
	"context"

	fwtmpl "github.com/opendatahub-io/operator-actions-framework/controller/actions/render/template"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	templateutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
)

const (
	ComponentKey    = fwtmpl.ComponentKey
	AppNamespaceKey = fwtmpl.AppNamespaceKey
)

type Action = fwtmpl.Action

type ActionOpts = fwtmpl.ActionOpts

var (
	WithCache       = fwtmpl.WithCache
	WithData        = fwtmpl.WithData
	WithDataFn      = fwtmpl.WithDataFn
	WithNamespaceFn = fwtmpl.WithNamespaceFn
	WithFuncMap     = fwtmpl.WithFuncMap
	WithLabel       = fwtmpl.WithLabel
	WithLabels      = fwtmpl.WithLabels
	WithAnnotation  = fwtmpl.WithAnnotation
	WithAnnotations = fwtmpl.WithAnnotations
)

// NewAction creates a new template render action with ODH defaults
// (ApplicationNamespace and TextTemplateFuncMap).
func NewAction(opts ...ActionOpts) actions.Fn {
	defaults := []ActionOpts{
		fwtmpl.WithNamespaceFn(func(ctx context.Context, rr *types.ReconciliationRequest) (string, error) {
			return cluster.ApplicationNamespace(ctx, rr.Client)
		}),
		fwtmpl.WithFuncMap(templateutils.TextTemplateFuncMap()),
	}
	return fwtmpl.NewAction(append(defaults, opts...)...)
}
