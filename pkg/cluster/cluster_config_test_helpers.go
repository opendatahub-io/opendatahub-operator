package cluster

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// TestClusterConfig holds test configuration for cluster config.
type TestClusterConfig struct {
	Platform             common.Platform
	ApplicationNamespace string
}

func SetTestClusterConfig(cfg TestClusterConfig) {
	clusterConfig.Release.Name = cfg.Platform
	clusterConfig.ApplicationNamespace = cfg.ApplicationNamespace
}

func ResetTestClusterConfig() {
	clusterConfig.ApplicationNamespace = ""
	clusterConfig.Release.Name = ""
}

func SetApplicationNamespaceForTest(ctx context.Context, cli client.Client) error {
	return setApplicationNamespace(ctx, cli)
}
