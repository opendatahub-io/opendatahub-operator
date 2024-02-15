package feature

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

//go:embed templates
var embeddedFiles embed.FS

const KustomizationFile = "kustomization.yaml"

var (
	BaseDir        = "templates"
	ServiceMeshDir = path.Join(BaseDir, "servicemesh")
	ServerlessDir  = path.Join(BaseDir, "serverless")
)

// Manifest defines the interface that all manifest types should implement.
type Manifest interface {
	Process(data interface{}) ([]*unstructured.Unstructured, error)
	isPatch() bool
}

type baseManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

func (b *baseManifest) isPatch() bool {
	return b.patch
}

// Ensure baseManifest implements the Manifest interface.
var _ Manifest = (*baseManifest)(nil)

func (b *baseManifest) Process(_ interface{}) ([]*unstructured.Unstructured, error) {
	manifestFile, err := b.fsys.Open(b.path)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()

	content, err := io.ReadAll(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	resources := string(content)

	var objs []*unstructured.Unstructured

	objs, err = convertToUnstructureds(resources, objs)
	if err != nil {
		return nil, err
	}
	return objs, nil
}

type templateManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

func (t *templateManifest) isPatch() bool {
	return t.patch
}

func (t *templateManifest) Process(data interface{}) ([]*unstructured.Unstructured, error) {
	manifestFile, err := t.fsys.Open(t.path)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()

	content, err := io.ReadAll(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	tmpl, err := template.New(t.name).Funcs(template.FuncMap{"ReplaceChar": ReplaceChar}).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return nil, err
	}

	resources := buffer.String()
	var objs []*unstructured.Unstructured

	objs, err = convertToUnstructureds(resources, objs)
	if err != nil {
		return nil, err
	}
	return objs, nil
}

// Ensure templateManifest implements the Manifest interface.
var _ Manifest = (*templateManifest)(nil)

// kustomizeManifest supports paths to kustomization files / directories containing a kustomization file
// note that it only supports to paths within the mounted files ie: /opt/manifests.
type kustomizeManifest struct {
	name,
	path string // path is to the directory containing a kustomization.yaml file within it or path to kust file itself
	fsys filesys.FileSystem
}

func (k *kustomizeManifest) isPatch() bool {
	return false
}

func (k *kustomizeManifest) Process(data interface{}) ([]*unstructured.Unstructured, error) {
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	var resMap resmap.ResMap
	resMap, resErr := kustomizer.Run(k.fsys, k.path)

	if resErr != nil {
		return nil, fmt.Errorf("error during resmap resources: %w", resErr)
	}

	targetNs := getTargetNs(data)
	if targetNs == "" {
		return nil, fmt.Errorf("error grabbing targetNs from feature spec")
	}

	if err := plugins.ApplyNamespacePlugin(targetNs, resMap); err != nil {
		return nil, err
	}

	// Todo: for now, this only supports applying labels to manifests source.Name when source.type==Component
	componentName := getComponentName(data)
	if componentName != "" {
		if err := plugins.ApplyAddLabelsPlugin(componentName, resMap); err != nil {
			return nil, err
		}
	}

	objs, resErr := deploy.GetResources(resMap)
	if resErr != nil {
		return nil, resErr
	}
	return objs, nil
}

// Ensure kustomizeManifest implements the Manifest interface.
var _ Manifest = (*kustomizeManifest)(nil)

func loadManifestsFrom(fsys fs.FS, path string) ([]Manifest, error) {
	var manifests []Manifest
	if isKustomizeManifest(path) {
		m := CreateKustomizeManifestFrom(path, nil)
		manifests = append(manifests, m)
		return manifests, nil
	}

	err := fs.WalkDir(fsys, path, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		_, err := dirEntry.Info()
		if err != nil {
			return err
		}

		if dirEntry.IsDir() {
			return nil
		}
		if isTemplateManifest(path) {
			manifests = append(manifests, CreateTemplateManifestFrom(fsys, path))
		} else {
			manifests = append(manifests, CreateBaseManifestFrom(fsys, path))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return manifests, nil
}

func CreateBaseManifestFrom(fsys fs.FS, path string) *baseManifest { //nolint:golint,revive //No need to export baseManifest.
	basePath := filepath.Base(path)
	m := &baseManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch"),
		fsys:  fsys,
	}

	return m
}

func CreateTemplateManifestFrom(fsys fs.FS, path string) *templateManifest { //nolint:golint,revive //No need to export templateManifest.
	basePath := filepath.Base(path)
	m := &templateManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch"),
		fsys:  fsys,
	}

	return m
}

func CreateKustomizeManifestFrom(path string, fsys filesys.FileSystem) *kustomizeManifest { //nolint:golint,revive //No need to export kustomizeManifest.
	basePath := filepath.Base(path)
	if basePath == KustomizationFile {
		path = filepath.Dir(path)
	}
	if fsys == nil {
		fsys = filesys.MakeFsOnDisk()
	}
	m := &kustomizeManifest{
		name: basePath,
		path: path,
		fsys: fsys,
	}

	return m
}

// parsing helpers
// isKustomizeManifest checks default filesystem for presence of kustomization file at this path.
func isKustomizeManifest(path string) bool {
	if filepath.Base(path) == KustomizationFile {
		return true
	}
	_, err := os.Stat(filepath.Join(path, KustomizationFile))
	return err == nil
}

func isTemplateManifest(path string) bool {
	return strings.Contains(path, ".tmpl")
}

func convertToUnstructureds(resources string, objs []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(resources, -1)
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			return nil, err
		}

		if !isNamespaceSet(u) {
			return nil, fmt.Errorf("no NS is set on %s", u.GetName())
		}
		objs = append(objs, u)
	}
	return objs, nil
}

// todo: rework these when spec is type map[string]any
func getTargetNs(data interface{}) string {
	if spec, ok := data.(*Spec); ok {
		return spec.TargetNamespace
	}
	return ""
}

func getComponentName(data interface{}) string {
	if featSpec, ok := data.(*Spec); ok {
		source := featSpec.Source
		if source == nil {
			return ""
		}
		if source.Type == featurev1.ComponentType {
			return source.Name
		}
		return ""
	}
	return ""
}
