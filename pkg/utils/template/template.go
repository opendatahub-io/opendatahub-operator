package template

import (
	"html/template"
	"strings"
	gt "text/template"
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
	}
}
