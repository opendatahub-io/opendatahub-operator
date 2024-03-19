package fixtures

import (
	"embed"
	"os"
	"path/filepath"
)

// TestEmbeddedFiles is an embedded filesystem that contains templates used specifically in tests files
//
//go:embed templates
var TestEmbeddedFiles embed.FS

const BaseDir = "templates"

// CreateFile creates a file with the given content in the specified directory.
func CreateFile(dir, filename, content string) error {
	filePath := filepath.Join(dir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}

	_, err = file.WriteString(content)
	if err != nil {
		return err
	}
	return file.Sync()
}
