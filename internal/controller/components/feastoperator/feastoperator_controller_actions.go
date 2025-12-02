package feastoperator

import (
	"context"
	"embed"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed monitoring
var monitoringFS embed.FS

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath(rr.Release.Name))
	return nil
}

// deployServiceMonitor deploys the ServiceMonitor for Feast operator metrics collection.
func deployServiceMonitor(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
		FS:   monitoringFS,
		Path: "monitoring/feastoperator-servicemonitor.tmpl.yaml",
	})

	return nil
}
