package modules

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// PlatformConfigPrefix is prepended to the module name to form the
	// ConfigMap name: odh-<modulename>-config.
	PlatformConfigPrefix = "odh-"

	// PlatformConfigSuffix is appended to the module name.
	PlatformConfigSuffix = "-config"

	// PlatformVersionKey is the data key containing the platform version.
	// This is platform-managed and reconciled back if modified externally.
	PlatformVersionKey = "platformVersion"
)

// PlatformConfigName returns the well-known ConfigMap name for a module.
func PlatformConfigName(moduleName string) string {
	return PlatformConfigPrefix + moduleName + PlatformConfigSuffix
}

// injectPlatformConfig creates or merges a per-module platform config
// ConfigMap into rr.Resources for each enabled module. The ConfigMap
// contains platform-managed fields (platformVersion) that the module
// controller reads to complete the version handshake.
//
// If the module's rendered manifests already include a ConfigMap with
// the same name (the optional controller configuration ConfigMap), the
// platform-managed keys are merged into it. Platform-managed keys are
// enforced via SSA on every reconcile — external modifications are
// reverted.
func injectPlatformConfig(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	platformVersion := rr.Release.Version.String()

	existingCMs := indexConfigMapsByName(rr.Resources)

	return reg.ForEach(func(handler ModuleHandler) error {
		if !handler.IsEnabled(platformCtx) {
			return nil
		}

		name := handler.GetName()
		cmName := PlatformConfigName(name)

		ns := platformCtx.ApplicationsNamespace

		if idx, ok := existingCMs[cmName]; ok {
			log.V(1).Info("merging platform config into existing ConfigMap",
				"module", name, "configmap", cmName)
			mergePlatformKeys(&rr.Resources[idx], platformVersion)
		} else {
			log.V(1).Info("creating platform config ConfigMap",
				"module", name, "configmap", cmName)
			cm := buildPlatformConfigMap(cmName, ns, platformVersion)
			u, err := toUnstructured(cm)
			if err != nil {
				return fmt.Errorf("converting platform config ConfigMap for %s: %w", name, err)
			}
			rr.Resources = append(rr.Resources, *u)
		}

		return nil
	})
}

func buildPlatformConfigMap(name, namespace, platformVersion string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			PlatformVersionKey: platformVersion,
		},
	}
}

// mergePlatformKeys sets the platform-managed keys on an existing
// unstructured ConfigMap. Existing module-owned keys are preserved.
func mergePlatformKeys(u *unstructured.Unstructured, platformVersion string) {
	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	if data == nil {
		data = make(map[string]string)
	}

	data[PlatformVersionKey] = platformVersion

	_ = unstructured.SetNestedStringMap(u.Object, data, "data")
}

func indexConfigMapsByName(resources []unstructured.Unstructured) map[string]int {
	idx := make(map[string]int)
	for i := range resources {
		if resources[i].GetKind() == "ConfigMap" {
			idx[resources[i].GetName()] = i
		}
	}
	return idx
}

func toUnstructured(obj any) (*unstructured.Unstructured, error) {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: raw}, nil
}
