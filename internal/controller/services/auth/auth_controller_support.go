package auth

import (
	"embed"
)

const (
	AdminGroupRoleTemplate          = "resources/data-science-admingroup-role.tmpl.yaml"
	AdminGroupIngressRoleTemplate   = "resources/data-science-admingroup-ingress-role.tmpl.yaml"
	AdminGroupClusterRoleTemplate   = "resources/data-science-admingroup-clusterrole.tmpl.yaml"
	AllowedGroupClusterRoleTemplate = "resources/data-science-allowedgroup-clusterrole.tmpl.yaml"
)

//go:embed resources
var resourcesFS embed.FS
