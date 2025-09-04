package auth

import (
	"embed"
)

const (
	AdminGroupRoleTemplate                     = "resources/admingroup-role.tmpl.yaml"
	AllowedGroupClusterRoleTemplate            = "resources/allowedgroup-clusterrole.tmpl.yaml"
	AdminGroupClusterRoleTemplate              = "resources/admingroup-clusterrole.tmpl.yaml"
	DataScienceMetricsAdminClusterRoleTemplate = "resources/data-science-metrics-admin-clusterrole.tmpl.yaml"
)

//go:embed resources
var resourcesFS embed.FS
