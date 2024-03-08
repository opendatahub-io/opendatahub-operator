package fixtures

import (
	"os"
	"path/filepath"
)

func CreateFile(dir, filename, data string) error {
	filePath := filepath.Join(dir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}

	_, err = file.WriteString(data)
	if err != nil {
		return err
	}
	return file.Sync()
}
