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
package common

import (
	"fmt"
	"os"
	"regexp"
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

func TrimToRFC1123Name(input string) string {
	if len(input) == 0 {
		return input
	}
	if len(input) > 63 {
		input = input[:63]
	}

	regex := regexp.MustCompile(`[^A-Za-z0-9\-]+`)
	replaced := regex.ReplaceAllString(input, "-")

	if !isAlphanumeric(replaced[0]) {
		replaced = "a" + replaced[1:]
	}

	if !isAlphanumeric(replaced[len(replaced)-1]) {
		replaced = replaced[:len(replaced)-1] + "z"
	}

	return strings.ToLower(replaced)
}

func isAlphanumeric(char byte) bool {
	regex := regexp.MustCompile(`^[A-Za-z0-9]$`)
	return regex.Match([]byte{char})
}
