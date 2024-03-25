package dscinitialization

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed resources
var dsciEmbeddedFS embed.FS

const baseDir = "resources"

var Templates = struct {
	// ServiceMeshDir is the path to the Service Mesh templates.
	ServiceMeshDir string
	// InstallDir is the path to the Serving install templates.
	AuthorinoDir string
	// GatewaysDir is the path to the Serving Istio gateways templates.
	MetricsDir string
	// Source the templates to be used
	Source fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshDir: path.Join(baseDir, "servicemesh"),
	AuthorinoDir:   path.Join(baseDir, "authorino"),
	MetricsDir:     path.Join(baseDir, "metrics-collection"),
	Source:         dsciEmbeddedFS,
	BaseDir:        baseDir,
}
