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
	// AlertManageDir is the path to the AlertManageConfigDir(OBO) templates.
	AlertManageDir string
	// MonitoringStackDir is the path to the MonitoringStack(OBO) templates.
	MonitoringStackDir string
	// Common is the path to the monitoring common templates.
	CommonDir string
	// SegmentDir is the path to the Segment-io templates.
	SegmentDir string
	// Location specifies the file system that contains the templates to be used.
	Location fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshDir:     path.Join(baseDir, "servicemesh"),
	AuthorinoDir:       path.Join(baseDir, "authorino"),
	MetricsDir:         path.Join(baseDir, "metrics-collection"),
	AlertManageDir:     path.Join(baseDir, "observability", "alertmanager"),
	MonitoringStackDir: path.Join(baseDir, "observability", "monitoringstack"),
	CommonDir:          path.Join(baseDir, "observability", "common"),
	SegmentDir:         path.Join(baseDir, "segment"),
	Location:           dsciEmbeddedFS,
	BaseDir:            baseDir,
}
