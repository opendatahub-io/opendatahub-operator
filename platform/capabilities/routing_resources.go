package capabilities

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed resources
var capabilityResourcesFS embed.FS

const baseDir = "resources"

var Templates = struct {
	// ServiceMeshIngressDir is the path to the Service Mesh Ingress templates.
	ServiceMeshIngressDir string
	// Location specifies the file system that contains the templates to be used.
	Location fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	ServiceMeshIngressDir: path.Join(baseDir, "servicemesh-ingress"),
	Location:              capabilityResourcesFS,
	BaseDir:               baseDir,
}
