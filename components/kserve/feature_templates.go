package kserve

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed templates
var kserveEmbeddedFS embed.FS

const baseDir = "templates"

var Templates = struct {
	// ServiceMeshDir is the path to the Service Mesh templates.
	ServiceMeshDir string
	// InstallDir is the path to the Serving install templates.
	InstallDir string
	// GatewaysDir is the path to the Serving Istio gateways templates.
	GatewaysDir string
	// Files the templates to be used
	Files fs.FS
}{
	ServiceMeshDir: path.Join(baseDir, "servicemesh"),
	InstallDir:     path.Join(baseDir, "serving-install"),
	GatewaysDir:    path.Join(baseDir, "serving-istio-gateways"),
	Files:         kserveEmbeddedFS,
}
