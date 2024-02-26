package feature

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

//go:embed templates
var embeddedFiles embed.FS

var (
	BaseDir        = "templates"
	ServiceMeshDir = path.Join(BaseDir, "servicemesh")
	ServerlessDir  = path.Join(BaseDir, "serverless")
)

type manifest struct {
	name,
	path,
	processedContent string
	template,
	patch bool
	fsys fs.FS
}

func loadManifestsFrom(fsys fs.FS, path string) ([]manifest, error) {
	var manifests []manifest

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
		m := createManifestFrom(fsys, path)
		manifests = append(manifests, m)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return manifests, nil
}

func createManifestFrom(fsys fs.FS, path string) manifest {
	basePath := filepath.Base(path)
	m := manifest{
		name:     basePath,
		path:     path,
		patch:    strings.Contains(basePath, ".patch"),
		template: filepath.Ext(path) == ".tmpl",
		fsys:     fsys,
	}

	return m
}

func (m *manifest) targetPath() string {
	return fmt.Sprintf("%s%s", m.path[:len(m.path)-len(filepath.Ext(m.path))], ".yaml")
}

func (m *manifest) process(data interface{}) error {
	manifestFile, err := m.open()
	if err != nil {
		return err
	}
	defer manifestFile.Close()

	content, err := io.ReadAll(manifestFile)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	if !m.template {
		// If, by convention, the file is not suffixed with `.tmpl` we do not need to trigger template processing.
		// It's safe to return at this point.
		m.processedContent = string(content)
		return nil
	}

	tmpl, err := template.New(m.name).Funcs(template.FuncMap{"ReplaceChar": ReplaceChar}).Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return err
	}

	m.processedContent = buffer.String()

	return nil
}

func (m *manifest) open() (fs.File, error) {
	return m.fsys.Open(m.path)
}
