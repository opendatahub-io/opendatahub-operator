package platformmodules

import (
	"maps"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
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

	CertManagerIssuerRefNameKey     = "CERT_MANAGER_ISSUER_REF_NAME"
	CertManagerIssuerRefKindKey     = "CERT_MANAGER_ISSUER_REF_KIND"
	CertManagerCASecretNameKey      = "CERT_MANAGER_CA_SECRET_NAME"      //nolint:gosec // ConfigMap key name, not a credential.
	CertManagerCASecretNamespaceKey = "CERT_MANAGER_CA_SECRET_NAMESPACE" //nolint:gosec // ConfigMap key name, not a credential.
	CertManagerIstioCACertPathKey   = "CERT_MANAGER_ISTIO_CA_CERT_PATH"
)

var platformManagedCertManagerKeys = []string{
	CertManagerIssuerRefNameKey,
	CertManagerIssuerRefKindKey,
	CertManagerCASecretNameKey,
	CertManagerCASecretNamespaceKey,
	CertManagerIstioCACertPathKey,
}

// PlatformConfigName returns the well-known ConfigMap name for a module.
func PlatformConfigName(moduleName string) string {
	return PlatformConfigPrefix + moduleName + PlatformConfigSuffix
}

func buildPlatformConfigMap(name, namespace, platformVersion string, extraParams map[string]string) *corev1.ConfigMap {
	data := map[string]string{
		PlatformVersionKey: platformVersion,
	}
	maps.Copy(data, extraParams)

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

// mergePlatformKeys sets the platform-managed keys on an existing
// unstructured ConfigMap. Existing module-owned keys are preserved.
func mergePlatformKeys(u *unstructured.Unstructured, platformVersion string, extraParams map[string]string) {
	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	if data == nil {
		data = make(map[string]string)
	}

	for _, k := range platformManagedCertManagerKeys {
		delete(data, k)
	}

	data[PlatformVersionKey] = platformVersion
	maps.Copy(data, extraParams)

	_ = unstructured.SetNestedStringMap(u.Object, data, "data")
}

func buildCertManagerConfigParams() map[string]string {
	bc := certmanager.DefaultBootstrapConfig()

	params := map[string]string{
		CertManagerIssuerRefNameKey:     bc.CAIssuerName,
		CertManagerIssuerRefKindKey:     bc.IssuerRefKind,
		CertManagerCASecretNameKey:      bc.CertName,
		CertManagerCASecretNamespaceKey: bc.CertManagerNamespace,
	}

	if v := os.Getenv(certmanager.EnvIstioCACertPath); v != "" {
		params[CertManagerIstioCACertPathKey] = v
	}

	return params
}

func indexConfigMapsByName(resources []unstructured.Unstructured) map[string]int {
	idx := make(map[string]int)
	for i := range resources {
		if resources[i].GetKind() == gvk.ConfigMap.Kind {
			idx[resources[i].GetName()] = i
		}
	}
	return idx
}
