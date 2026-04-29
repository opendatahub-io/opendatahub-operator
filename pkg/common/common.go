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
	"embed"
	"io/fs"
	"slices"
	"strings"
)

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
	for e := range strings.SplitSeq(list, ",") {
		added += sliceAddMissing(s, e)
	}
	return added
}

// FileExists checks if a file exists in the embedded filesystem.
func FileExists(fsys embed.FS, path string) bool {
	_, err := fs.Stat(fsys, path)
	return err == nil
}
