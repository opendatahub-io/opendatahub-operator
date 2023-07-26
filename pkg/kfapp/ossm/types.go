package ossm

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

const (
	TMPL_LOCAL_PATH = "service-mesh/templates"
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

func (m *manifest) targetPath() string {
	return fmt.Sprintf("%s%s", m.path[:len(m.path)-len(filepath.Ext(m.path))], ".yaml")
}

func (m *manifest) processTemplate(data interface{}) error {
	if !m.template {
		return nil
	}
	f, err := os.Create(m.targetPath())
	if err != nil {
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
