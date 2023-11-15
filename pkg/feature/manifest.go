package feature

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

const (
	BaseDir         = "templates/servicemesh/"
	ControlPlaneDir = BaseDir + "control-plane"
	AuthDir         = BaseDir + "authorino"
	MonitoringDir   = BaseDir + "monitoring"
	BaseOutputDir   = "/tmp/opendatahub-manifests/"
)

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
		m := loadManifestFrom(path)
		manifests = append(manifests, m)

		return nil
	}); err != nil {
		return nil, errors.WithStack(err)
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

func (m *manifest) processTemplate(data interface{}) error {
	if !m.template {
		return nil
	}
	path := m.targetPath()

	f, err := os.Create(path)
	if err != nil {
		log.Error(err, "Failed to create file")

		return err
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
