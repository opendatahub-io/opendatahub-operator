package kserve

import (
	"embed"
	"path"
)

//go:embed templates
var kserveEmbeddedFS embed.FS

var (
	baseDir        = "templates"
	serviceMeshDir = path.Join(baseDir, "servicemesh")
	installDir     = path.Join(baseDir, "serving-install")
	gatewaysDir    = path.Join(baseDir, "serving-istio-gateways")
)
