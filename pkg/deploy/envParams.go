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
parameter isUpdateNamespace is used to set if should update namespace  with DSCI CR's applicationnamespace.
extraParamsMaps is used to set extra parameters which are not carried from ENV variable. this can be passed per component.
*/
func ApplyParams(componentPath string, imageParamsMap map[string]string, isUpdateNamespace bool, extraParamsMaps ...map[string]string) error {
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

	// 2. Update namespace variable with applicationNamepsace
	if isUpdateNamespace {
		paramsEnvMap["namespace"] = imageParamsMap["namespace"]
	}

	// 3. Update other fileds with extraParamsMap which are not carried from component
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
