/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package common contains utility functions used by different components
// for cluster related common operations, refer to package cluster
package common

import (
	"crypto/sha256"
	"embed"
	b64 "encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
)

// ReplaceStringsInFile replaces variable with value in manifests during runtime.
func ReplaceStringsInFile(fileName string, replacements map[string]string) error {
	// Read the contents of the file
	fileContent, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Replace all occurrences of the strings in the map
	newContent := string(fileContent)
	for string1, string2 := range replacements {
		newContent = strings.ReplaceAll(newContent, string1, string2)
	}

	// Write the modified content back to the file
	err = os.WriteFile(fileName, []byte(newContent), 0)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	return nil
}

// MatchLineInFile use the 'key' of the replacements as match pattern and replace the line with 'value'.
func MatchLineInFile(fileName string, replacements map[string]string) error {
	fileContent, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Pre-compile all regex patterns to avoid compilation in loop
	compiledRegexes := make(map[*regexp.Regexp]string, len(replacements))
	for matchPattern, newValue := range replacements {
		re, err := regexp.Compile(matchPattern + `(.*)`)
		if err != nil {
			return fmt.Errorf("failed to compile regex pattern %q: %w", matchPattern, err)
		}
		compiledRegexes[re] = newValue
	}

	newContent := string(fileContent)
	for re, newValue := range compiledRegexes {
		newContent = re.ReplaceAllString(newContent, newValue)
	}

	err = os.WriteFile(fileName, []byte(newContent), 0)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	return nil
}

// encode configmap data and return in base64.
func GetMonitoringData(data string) (string, error) {
	// Create a new SHA-256 hash object
	hash := sha256.New()

	// Write the input data to the hash object
	_, err := hash.Write([]byte(data))
	if err != nil {
		return "", err
	}

	// Get the computed hash sum
	hashSum := hash.Sum(nil)

	// Encode the hash sum to Base64
	encodedData := b64.StdEncoding.EncodeToString(hashSum)

	return encodedData, nil
}

func sliceAddMissing(s *[]string, e string) int {
	e = strings.TrimSpace(e)
	if slices.Contains(*s, e) {
		return 0
	}
	*s = append(*s, e)
	return 1
}

// adds elements of comma separated list.
func AddMissing(s *[]string, list string) int {
	added := 0
	for _, e := range strings.Split(list, ",") {
		added += sliceAddMissing(s, e)
	}
	return added
}

// FileExists checks if a file exists in the embedded filesystem.
func FileExists(fsys embed.FS, path string) bool {
	_, err := fs.Stat(fsys, path)
	return err == nil
}
