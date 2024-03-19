package dscinitialization

import (
	"embed"
	"path"
)

//go:embed templates
var dsciEmbeddedFS embed.FS

var (
	baseDir      = "templates"
	authorinoDir = path.Join(baseDir, "authorino")
	meshDir      = path.Join(baseDir, "mesh")
	metricsDir   = path.Join(baseDir, "metrics-collection")
)
