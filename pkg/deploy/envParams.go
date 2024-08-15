package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
overwrite values in components' manifests params.env file
This is useful for air gapped cluster
priority of image values (from high to low):
- image values set in manifests params.env if manifestsURI is set
- RELATED_IMAGE_* values from CSV (if it is set)
- image values set in manifests params.env if manifestsURI is not set.
extraParamsMaps is used to set extra parameters which are not carried from ENV variable. this can be passed per component.
*/
func ApplyParams(componentPath string, imageParamsMap map[string]string, extraParamsMaps ...map[string]string) error {
	paramsFile := filepath.Join(componentPath, "params.env")
	// Require params.env at the root folder
	paramsEnv, err := os.Open(paramsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// params.env doesn't exist, do not apply any changes
			return nil
		}
		return err
	}

	defer paramsEnv.Close()

	paramsEnvMap := make(map[string]string)
	scanner := bufio.NewScanner(paramsEnv)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			paramsEnvMap[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// 1. Update images with env variables
	// e.g "odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	for i := range paramsEnvMap {
		relatedImageValue := os.Getenv(imageParamsMap[i])
		if relatedImageValue != "" {
			paramsEnvMap[i] = relatedImageValue
		}
	}

	// 2. Update other fileds with extraParamsMap which are not carried from component
	for _, extraParamsMap := range extraParamsMaps {
		for eKey, eValue := range extraParamsMap {
			paramsEnvMap[eKey] = eValue
		}
	}

	// Move the existing file to a backup file and create empty file
	paramsBackupFile := paramsFile + ".bak"
	if err := os.Rename(paramsFile, paramsBackupFile); err != nil {
		return err
	}

	file, err := os.Create(paramsFile)
	if err != nil {
		// If create fails, try to restore the backup file
		_ = os.Rename(paramsBackupFile, paramsFile)
		return err
	}
	defer file.Close()

	// Now, write the new map back to params.env
	writer := bufio.NewWriter(file)
	for key, value := range paramsEnvMap {
		if _, fErr := fmt.Fprintf(writer, "%s=%s\n", key, value); fErr != nil {
			return fErr
		}
	}
	if err := writer.Flush(); err != nil {
		if removeErr := os.Remove(paramsFile); removeErr != nil {
			fmt.Printf("Failed to remove file: %v", removeErr)
		}
		if renameErr := os.Rename(paramsBackupFile, paramsFile); renameErr != nil {
			fmt.Printf("Failed to restore file from backup: %v", renameErr)
		}
		fmt.Printf("Failed to write to file: %v", err)
		return err
	}

	// cleanup backup file params.env.bak
	if err := os.Remove(paramsBackupFile); err != nil {
		fmt.Printf("Failed to remove backup file: %v", err)
		return err
	}

	return nil
}

// check if matchStrings exists in componentPath's params.env file
// if found string in matchStrings all exist, return nil
// if any step fail, return err.
// if some of the strings are not found, return err.
func CheckParams(componentPath string, matchStrings []string) error {
	paramsFile := filepath.Join(componentPath, "params.env")
	paramsEnv, err := os.Open(paramsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return err
	}
	defer paramsEnv.Close()

	// init a all false map
	found := make(map[string]bool)
	for _, str := range matchStrings {
		found[str] = false
	}
	scanner := bufio.NewScanner(paramsEnv)
	for scanner.Scan() {
		line := scanner.Text()
		for _, str := range matchStrings {
			if strings.Contains(line, str) {
				found[str] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	var allMissings []string
	for _, str := range matchStrings {
		if !found[str] {
			allMissings = append(allMissings, str)
		}
	}
	if len(allMissings) > 0 {
		return fmt.Errorf("such are not found in params.env: %v", allMissings)
	}
	return nil
}
