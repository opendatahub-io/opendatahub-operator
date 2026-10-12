package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	// DatabaseServiceComponentName is the DSC/Platform JSON key and module registry name.
	DatabaseServiceComponentName = "databaseservice"

	// DatabaseServiceInstanceName is the cluster-scoped DatabaseService singleton.
	// It must match the CEL rule on the module CRD: metadata.name == default-db-operator.
	DatabaseServiceInstanceName = "default-db-operator"

	// DatabaseServiceKind is the Kubernetes kind of the module CR
	// (services.platform.opendatahub.io).
	DatabaseServiceKind = "DatabaseService"
)

// DatabaseServiceCommonSpec is empty: the DatabaseService module CR has no
// configurable desired state. Do not add DSC-only fields here.
type DatabaseServiceCommonSpec struct{}

// DatabaseServiceCommonStatus is the shared observed state mirrored onto DSC.
type DatabaseServiceCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCDatabaseService is the configuration exposed on DataScienceCluster.
type DSCDatabaseService struct {
	common.ManagementSpec     `json:",inline"`
	DatabaseServiceCommonSpec `json:",inline"`
}

// DSCDatabaseServiceStatus is the observed state exposed on DataScienceCluster.
type DSCDatabaseServiceStatus struct {
	common.ManagementSpec        `json:",inline"`
	*DatabaseServiceCommonStatus `json:",inline"`
}
