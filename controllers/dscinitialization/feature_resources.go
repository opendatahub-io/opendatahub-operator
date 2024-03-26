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
	// ServiceMonitorDir is the path to the ServiceMonitor(OBO) templates.
	ServiceMonitorDir string
	// SegmentDir is the path to the Segment-io templates.
	SegmentDir string
	// PrometheusRuleDir is the path to the PrometheusRule(OBO) templates.
	PrometheusRuleDir string
	// Source the templates to be used
	Source fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshDir:     path.Join(baseDir, "servicemesh"),
	AuthorinoDir:       path.Join(baseDir, "authorino"),
	MetricsDir:         path.Join(baseDir, "metrics-collection"),
	AlertManageDir:     path.Join(baseDir, "observibility", "alertmanager"),
	MonitoringStackDir: path.Join(baseDir, "observibility", "monitoringstack"),
	ServiceMonitorDir:  path.Join(baseDir, "observibility", "servicemonitor"),
	PrometheusRuleDir:  path.Join(baseDir, "observibility", "prometheusrule"),
	SegmentDir:         path.Join(baseDir, "segment"),
	Source:             dsciEmbeddedFS,
	BaseDir:            baseDir,
}
