package servicemesh

import (
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"path/filepath"
)

const (
	gatewayPattern = `ISTIO_GATEWAY=(.*)`
)

// OverwriteIstioGatewayVar replaces the ISTIO_GATEWAY with given namespace and "odh-gateway" in the specified ossm.env file.
// This is used in conjunction with kustomize overlays for Kubeflow notebook controllers. By overwritting referenced we can set
// proper values for environment variables populated through Kustomize.
func OverwriteIstioGatewayVar(namespace, path string) error {
	envFile := filepath.Join(path, "ossm.env")
	replacement := fmt.Sprintf("ISTIO_GATEWAY=%s", namespace+"/odh-gateway")

	return common.ReplaceInFile(envFile, map[string]string{gatewayPattern: replacement})
}
