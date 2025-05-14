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
	authorinoDeploymentInjectionTemplate     = path.Join(baseDir, authorinoDir, "deployment.injection.patch.tmpl.yaml")
	authorinoServiceMeshControlPlaneTemplate = path.Join(baseDir, authorinoDir, "mesh-authz-ext-provider.patch.tmpl.yaml")

	podMonitorTemplate     = path.Join(baseDir, metricsDir, "envoy-metrics-collection.tmpl.yaml")
	serviceMonitorTemplate = path.Join(baseDir, metricsDir, "pilot-metrics-collection.tmpl.yaml")

	serviceMeshControlPlaneTemplate = path.Join(baseDir, serviceMeshDir, "create-smcp.tmpl.yaml")
)

// var smcpReadyPredicate = predicate.Funcs{
//	UpdateFunc: func(e event.UpdateEvent) bool {
//		if e.ObjectOld == nil || e.ObjectNew == nil {
//			return false
//		}
//
//		oldSmcpReady := isSmcpReady(e.ObjectOld)
//		newSmcpReady := isSmcpReady(e.ObjectNew)
//
//		if oldSmcpReady != newSmcpReady {
//			return true
//		}
//
//		generationIncreased := e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
//		if generationIncreased {
//			if !oldSmcpReady && !newSmcpReady {
//				return false
//			}
//			return true
//		}
//
//		return false
//	},
//	CreateFunc:  func(e event.CreateEvent) bool { return true },
//	DeleteFunc:  func(e event.DeleteEvent) bool { return true },
//	GenericFunc: func(e event.GenericEvent) bool { return true },
//}
//
// func isSmcpReady(obj client.Object) bool {
//	smcp, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
//	if err != nil {
//		return false
//	}
//
//	conditions, found, err := unstructured.NestedSlice(smcp, "status", "conditions")
//	if err != nil || !found || len(conditions) == 0 {
//		return false
//	}
//
//	for _, condition := range conditions {
//		conditionMap, ok := condition.(map[string]interface{})
//		if !ok {
//			continue
//		}
//
//		typeVal, typeOk := conditionMap["type"].(string)
//		statusVal, statusOk := conditionMap["status"].(string)
//		if typeOk && statusOk && typeVal == "Ready" && statusVal == string(metav1.ConditionTrue) {
//			return true
//		}
//	}
//
//	return false
//}
