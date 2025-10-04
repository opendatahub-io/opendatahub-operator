package auth

import (
	"embed"
)

const (
	AdminGroupRoleTemplate          = "resources/admingroup-role.tmpl.yaml"
	AdminGroupClusterRoleTemplate   = "resources/admingroup-clusterrole.tmpl.yaml"
	AllowedGroupClusterRoleTemplate = "resources/allowedgroup-clusterrole.tmpl.yaml"
)

//go:embed resources
var resourcesFS embed.FS
