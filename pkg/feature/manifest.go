package feature

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

const BaseOutputDir = "/tmp/odh-operator"

type manifest struct {
	name,
	path string
	template,
	patch,
	processed bool
}

func loadManifestsFrom(path string) ([]manifest, error) {
	var manifests []manifest
	if err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		basePath := filepath.Base(path)
		manifests = append(manifests, manifest{
			name:     basePath,
			path:     path,
			patch:    strings.Contains(basePath, ".patch"),
			template: filepath.Ext(path) == ".tmpl",
		})
		return nil
	}); err != nil {
		return nil, errors.WithStack(err)
	}

	return manifests, nil
}

func (m *manifest) targetPath() string {
	return fmt.Sprintf("%s%s", m.path[:len(m.path)-len(filepath.Ext(m.path))], ".yaml")
}

func (m *manifest) processTemplate(data interface{}) error {
	if !m.template {
		return nil
	}
	path := m.targetPath()

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	tmpl := template.New(m.name).Funcs(template.FuncMap{"ReplaceChar": ReplaceChar})

	tmpl, err = tmpl.ParseFiles(m.path)
	if err != nil {
		return err
	}

	err = tmpl.Execute(f, data)
	m.processed = err == nil

	return err
}
