package feature

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed templates
var embeddedFiles embed.FS

type manifest struct {
	name,
	path,
	processedContent string
	template,
	patch bool
}

func loadManifestsFrom(fsys fs.FS, path string) ([]manifest, error) {
	var manifests []manifest

	err := fs.WalkDir(fsys, path, func(path string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if dirEntry.IsDir() {
			return nil
		}

		_, err = fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		m := loadManifestFrom(path)
		manifests = append(manifests, m)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return manifests, nil
}

func loadManifestFrom(path string) manifest {
	basePath := filepath.Base(path)
	m := manifest{
		name:     basePath,
		path:     path,
		patch:    strings.Contains(basePath, ".patch"),
		template: filepath.Ext(path) == ".tmpl",
	}

	return m
}

func (m *manifest) targetPath() string {
	return fmt.Sprintf("%s%s", m.path[:len(m.path)-len(filepath.Ext(m.path))], ".yaml")
}

func (m *manifest) process(fsys fs.FS, data interface{}) error {
	manifestFile, err := fsys.Open(m.path)
	if err != nil {
		log.Error(err, "Failed to open manifest file", "path", m.path)
		return err
	}
	defer manifestFile.Close()

	content, err := io.ReadAll(manifestFile)
	if err != nil {
		log.Error(err, "Failed to read manifest file", "path", m.path)
		return err
	}

	if !m.template {
		// If, by convention, the file is not suffixed with `.tmpl` we do not need to trigger template processing.
		// It's safe to return at this point.
		m.processedContent = string(content)
		return nil
	}

	tmpl, err := template.New(m.name).Funcs(template.FuncMap{"ReplaceChar": ReplaceChar}).Parse(string(content))
	if err != nil {
		log.Error(err, "Failed to template for file", "path", m.path)
		return err
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return err
	}

	m.processedContent = buffer.String()

	return nil
}
