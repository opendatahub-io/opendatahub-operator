package dscinitialization

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed templates
var dsciEmbeddedFS embed.FS

const baseDir = "templates"

var Templates = struct {
	// ServiceMeshDir is the path to the Service Mesh templates.
	ServiceMeshDir string
	// InstallDir is the path to the Serving install templates.
	AuthorinoDir string
	// GatewaysDir is the path to the Serving Istio gateways templates.
	MetricsDir string
	// Files the templates to be used
	Files fs.FS
}{
	ServiceMeshDir: path.Join(baseDir, "servicemesh"),
	AuthorinoDir:   path.Join(baseDir, "authorino"),
	MetricsDir:     path.Join(baseDir, "metrics-collection"),
	Files:          dsciEmbeddedFS,
}
