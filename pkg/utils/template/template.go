package template

import (
	"bytes"
	"html/template"
	"io/fs"
	"path"
	"strings"
	gt "text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Indent adds the specified number of spaces to each line of the input string.
func Indent(spaces int, text string) string {
	if text == "" {
		return text
	}
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// HTMLTemplateFuncMap returns a map of custom template functions for html/template.
func HTMLTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"indent": Indent,
	}
}

// TextTemplateFuncMap returns a map of custom template functions for text/template.
func TextTemplateFuncMap() gt.FuncMap {
	return gt.FuncMap{
		"indent": Indent,
		"nindent": func(spaces int, s string) string {
			if s == "" {
				return ""
			}
			return "\n" + Indent(spaces, s)
		},
		"toYaml": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			return string(b), err
		},
	}
}

// RenderObject renders a YAML template from fs into an unstructured object.
func RenderObject(templateFS fs.FS, templatePath string, data any) (*unstructured.Unstructured, error) {
	parsed, err := gt.New("fixture").Option("missingkey=error").Funcs(TextTemplateFuncMap()).ParseFS(templateFS, templatePath)
	if err != nil {
		return nil, err
	}

	var rendered bytes.Buffer
	if err := parsed.ExecuteTemplate(&rendered, path.Base(templatePath), data); err != nil {
		return nil, err
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(rendered.Bytes(), obj); err != nil {
		return nil, err
	}

	return obj, nil
}
