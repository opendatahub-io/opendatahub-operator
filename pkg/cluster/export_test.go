package cluster

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

// ResetConfigForTest clears package-level state set by Init between tests.
func ResetConfigForTest() {
	clusterConfig.Namespace = ""
	clusterConfig.ApplicationNamespace = ""
	clusterConfig.Release = common.Release{}
	clusterConfig.ClusterInfo = ClusterInfo{}
}
