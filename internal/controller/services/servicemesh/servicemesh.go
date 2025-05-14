package servicemesh

import (
	"embed"
	"path"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

const (
	ServiceName = "servicemesh"
)

var (
	conditionTypes = []string{
		status.CapabilityServiceMesh,
		status.CapabilityServiceMeshAuthorization,
	}
)

//go:embed resources
var resourcesFS embed.FS

const (
	authRefsConfigMapName = "auth-refs"
	meshRefsConfigMapName = "service-mesh-refs"

	authProviderName = "authorino"
	authorinoLabel   = "security.opendatahub.io/authorization-group=default"
)

const (
	baseDir = "resources"

	authorinoDir   = "authorino"
	metricsDir     = "metrics-collection"
	serviceMeshDir = "servicemesh"

	authorinoOperatorName   = "authorino-operator"
	serviceMeshOperatorName = "servicemeshoperator"
)

var (
	authorinoTemplate                        = path.Join(baseDir, authorinoDir, "base/operator-cluster-wide-no-tls.tmpl.yaml")
	authorinoServiceMeshMemberTemplate       = path.Join(baseDir, authorinoDir, "auth-smm.tmpl.yaml")
	authorinoServiceMeshControlPlaneTemplate = path.Join(baseDir, authorinoDir, "mesh-authz-ext-provider.patch.tmpl.yaml")

	podMonitorTemplate     = path.Join(baseDir, metricsDir, "envoy-metrics-collection.tmpl.yaml")
	serviceMonitorTemplate = path.Join(baseDir, metricsDir, "pilot-metrics-collection.tmpl.yaml")

	serviceMeshControlPlaneTemplate = path.Join(baseDir, serviceMeshDir, "create-smcp.tmpl.yaml")
)
