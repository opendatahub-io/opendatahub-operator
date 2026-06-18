package kserve

import (
	"context"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	ippGatewayNamespace = "openshift-ingress"
	ippGatewayName      = "maas-default-gateway"
	ippMaaSAPIRouteName = "maas-api-route"

	ippPayloadProcessingName    = "payload-processing"
	ippPayloadPreProcessingName = "payload-pre-processing"
	ippGRPCPort                 = 9004
)

var ippResourceNames = map[string]bool{
	"payload-processing":         true,
	"payload-pre-processing":     true,
	"payload-processing-plugins": true,
}

var ippClusterScopedNames = map[string]bool{
	"payload-processing-reader": true,
}

func isIPPResource(r unstructured.Unstructured) bool {
	name := r.GetName()
	return ippResourceNames[name] || ippClusterScopedNames[name]
}

func customizeIPPResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kserve)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kserve", rr.Instance)
	}

	if k.Spec.InferencePayloadProcessing.ManagementState != operatorv1.Managed {
		return removeIPPResources(rr)
	}

	return patchIPPResources(rr)
}

func removeIPPResources(rr *odhtypes.ReconciliationRequest) error {
	filtered := make([]unstructured.Unstructured, 0, len(rr.Resources))
	for _, r := range rr.Resources {
		if !isIPPResource(r) {
			filtered = append(filtered, r)
		}
	}
	rr.Resources = filtered
	return nil
}

func patchIPPResources(rr *odhtypes.ReconciliationRequest) error {
	l := logf.Log.WithName("ipp")
	appNamespace := cluster.GetApplicationNamespace()

	for i := range rr.Resources {
		r := &rr.Resources[i]
		name := r.GetName()

		if !isIPPResource(*r) {
			continue
		}

		gvk := r.GroupVersionKind()

		if ippClusterScopedNames[name] {
			if gvk.Kind == "ClusterRoleBinding" {
				patchCRBSubjectNS(r, ippGatewayNamespace)
			}
			continue
		}

		l.V(4).Info("Setting IPP resource namespace", "name", name, "namespace", ippGatewayNamespace)
		r.SetNamespace(ippGatewayNamespace)

		switch {
		case gvk.Kind == "EnvoyFilter" && name == ippPayloadProcessingName:
			if err := patchIPPEnvoyFilter(r, appNamespace); err != nil {
				return fmt.Errorf("patch IPP EnvoyFilter: %w", err)
			}
		case gvk.Kind == "DestinationRule":
			patchIPPDestinationRuleHost(r)
		}
	}

	return nil
}

func patchIPPEnvoyFilter(r *unstructured.Unstructured, appNamespace string) error {
	// Patch targetRefs gateway name
	targetRefs, found, err := unstructured.NestedSlice(r.Object, "spec", "targetRefs")
	if err != nil {
		return fmt.Errorf("read targetRefs: %w", err)
	}
	if found && len(targetRefs) > 0 {
		ref, ok := targetRefs[0].(map[string]any)
		if ok {
			ref["name"] = ippGatewayName
			targetRefs[0] = ref
			_ = unstructured.SetNestedSlice(r.Object, targetRefs, "spec", "targetRefs")
		}
	}

	configPatches, found, err := unstructured.NestedSlice(r.Object, "spec", "configPatches")
	if err != nil {
		return fmt.Errorf("read configPatches: %w", err)
	}
	if !found || len(configPatches) < 4 {
		return fmt.Errorf("expected at least 4 configPatches, got %d", len(configPatches))
	}

	anchorName := fmt.Sprintf("extensions.istio.io/wasmplugin/%s.kuadrant-%s", ippGatewayNamespace, ippGatewayName)
	preCluster := fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local", ippGRPCPort, ippPayloadPreProcessingName, ippGatewayNamespace)
	postCluster := fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local", ippGRPCPort, ippPayloadProcessingName, ippGatewayNamespace)

	clusters := []string{preCluster, postCluster}

	// Patches 0 and 1: INSERT_BEFORE and INSERT_AFTER with gRPC clusters and WasmPlugin anchor
	for i, clusterName := range clusters {
		patch, ok := configPatches[i].(map[string]any)
		if !ok {
			return fmt.Errorf("configPatches[%d] is not a map", i)
		}
		_ = unstructured.SetNestedField(patch, anchorName,
			"match", "listener", "filterChain", "filter", "subFilter", "name")
		_ = unstructured.SetNestedField(patch, clusterName,
			"patch", "value", "typed_config", "grpc_service", "envoy_grpc", "cluster_name")
		configPatches[i] = patch
	}

	// Patches 2 and 3: disable ext_proc on non-inference routes
	for i := 2; i < 4; i++ {
		patch, ok := configPatches[i].(map[string]any)
		if !ok {
			return fmt.Errorf("configPatches[%d] is not a map", i)
		}
		routeName := fmt.Sprintf("%s.%s.%d", appNamespace, ippMaaSAPIRouteName, i-2)
		_ = unstructured.SetNestedField(patch, routeName,
			"match", "routeConfiguration", "vhost", "route", "name")
		configPatches[i] = patch
	}

	return unstructured.SetNestedSlice(r.Object, configPatches, "spec", "configPatches")
}

func patchIPPDestinationRuleHost(r *unstructured.Unstructured) {
	host, found, _ := unstructured.NestedString(r.Object, "spec", "host")
	if !found || host == "" {
		return
	}
	// Replace namespace segment in FQDN: name.OLD_NS.svc.cluster.local → name.NEW_NS.svc.cluster.local
	parts := strings.SplitN(host, ".", 3)
	if len(parts) >= 2 {
		parts[1] = ippGatewayNamespace
		_ = unstructured.SetNestedField(r.Object, strings.Join(parts, "."), "spec", "host")
	}
}

func patchCRBSubjectNS(r *unstructured.Unstructured, namespace string) {
	subjects, found, _ := unstructured.NestedSlice(r.Object, "subjects")
	if !found {
		return
	}
	for i, s := range subjects {
		subj, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		subj["namespace"] = namespace
		subjects[i] = subj
	}
	_ = unstructured.SetNestedSlice(r.Object, subjects, "subjects")
}
