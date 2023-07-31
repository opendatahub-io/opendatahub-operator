package ossm

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type oAuth struct {
	AuthzEndpoint,
	TokenEndpoint,
	Route,
	Port,
	ClientSecret,
	Hmac string
}

type manifest struct {
	name,
	path string
	template,
	patch,
	processed bool
}

const (
	ControlPlaneDir = "templates/control-plane"
	AuthDir         = "templates/authorino"
	baseOutputDir   = "/tmp/ossm-installer/"
)

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

	tmpl := template.New(m.name).
		Funcs(template.FuncMap{"ReplaceChar": ReplaceChar})

	tmpl, err = tmpl.ParseFiles(m.path)
	if err != nil {
		return err
	}

	err = tmpl.Execute(f, data)
	m.processed = err == nil

	return err
}

func ReplaceChar(s string, oldChar, newChar string) string {
	return strings.ReplaceAll(s, oldChar, newChar)
}

// In order to process the templates, we need to create a tmp directory
// to store the files. This is because embedded files are read only.
// copyEmbeddedFS ensures that files embedded using go:embed are populated
// to dest directory
func copyEmbeddedFS(fsys fs.FS, root, dest string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(dest, path)
		if d.IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
		} else {
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return err
			}
		}

		return nil
	})
}
