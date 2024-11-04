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
	// AuthorinoDir is the path to the Authorino templates.
	AuthorinoDir string
	// MetricsDir is the path to the Metrics Collection templates.
	MetricsDir string
	// Location specifies the file system that contains the templates to be used.
	Location fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshDir: path.Join(baseDir, "servicemesh"),
	AuthorinoDir:   path.Join(baseDir, "authorino"),
	MetricsDir:     path.Join(baseDir, "metrics-collection"),
	Location:       dsciEmbeddedFS,
	BaseDir:        baseDir,
}
