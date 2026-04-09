package auth

import (
	"embed"
)

const (
	AdminGroupRoleTemplate          = "resources/data-science-admingroup-role.tmpl.yaml"
	AdminGroupMaaSRoleTemplate      = "resources/data-science-admingroup-maas-role.tmpl.yaml"
	AdminGroupClusterRoleTemplate   = "resources/data-science-admingroup-clusterrole.tmpl.yaml"
	AllowedGroupClusterRoleTemplate = "resources/data-science-allowedgroup-clusterrole.tmpl.yaml"
	AdminGroupKuadrantRoleTemplate  = "resources/data-science-admingroup-kuadrant-role.tmpl.yaml"
)

//go:embed resources
var resourcesFS embed.FS
