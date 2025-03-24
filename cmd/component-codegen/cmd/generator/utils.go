package generator

import (
	"os"
)

const FilePerm = 0644

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
