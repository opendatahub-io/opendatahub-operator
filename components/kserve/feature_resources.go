package kserve

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed resources
var kserveEmbeddedFS embed.FS

const baseDir = "resources"

var Resources = struct {
	// ServiceMeshDir is the path to the Service Mesh templates.
	ServiceMeshDir string
	// InstallDir is the path to the Serving install templates.
	InstallDir string
	// GatewaysDir is the path to the Serving Istio gateways templates.
	GatewaysDir string
	// Source the templates to be used
	Source fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshDir: path.Join(baseDir, "servicemesh"),
	InstallDir:     path.Join(baseDir, "serving-install"),
	GatewaysDir:    path.Join(baseDir, "servicemesh", "routing"),
	Source:         kserveEmbeddedFS,
	BaseDir:        baseDir,
}
