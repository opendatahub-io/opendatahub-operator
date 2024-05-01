package feature

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Manifest interface {
	// Process allows any arbitrary struct to be passed and used while processing the content of the manifest.
	Process(data any) ([]*unstructured.Unstructured, error)
}

type baseManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

var _ Manifest = (*baseManifest)(nil)

func (b *baseManifest) Process(_ any) ([]*unstructured.Unstructured, error) {
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

	return convertToUnstructuredSlice(resources)
}

var _ Manifest = (*templateManifest)(nil)

type templateManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

func (t *templateManifest) Process(data any) ([]*unstructured.Unstructured, error) {
	manifestFile, err := t.fsys.Open(t.path)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()

	content, err := io.ReadAll(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	tmpl, err := template.New(t.name).
		Option("missingkey=error").
		Funcs(template.FuncMap{"ReplaceChar": ReplaceChar}).
		Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	resources := buffer.String()

	return convertToUnstructuredSlice(resources)
}

func loadManifestsFrom(fsys fs.FS, path string) ([]Manifest, error) {
	var manifests []Manifest

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

	return &baseManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch"),
		fsys:  fsys,
	}
}

func CreateTemplateManifestFrom(fsys fs.FS, path string) *templateManifest { //nolint:golint,revive //No need to export templateManifest.
	basePath := filepath.Base(path)

	return &templateManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch."),
		fsys:  fsys,
	}
}

func isTemplateManifest(path string) bool {
	return strings.Contains(filepath.Base(path), ".tmpl.")
}

func convertToUnstructuredSlice(resources string) ([]*unstructured.Unstructured, error) {
	splitter := regexp.MustCompile(YamlSeparator)
	objectStrings := splitter.Split(resources, -1)
	objs := make([]*unstructured.Unstructured, 0, len(objectStrings))
	for _, str := range objectStrings {
		if strings.TrimSpace(str) == "" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(str), u); err != nil {
			return nil, err
		}

		objs = append(objs, u)
	}
	return objs, nil
}
