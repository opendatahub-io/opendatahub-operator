package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func parseParams(fileName string) (map[string]string, error) {
	paramsEnv, err := os.Open(fileName)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return paramsEnvMap, nil
}

func writeParamsToTmp(params map[string]string, tmpDir string) (string, error) {
	tmp, err := os.CreateTemp(tmpDir, "params.env-")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	// Write the new map to temporary file
	writer := bufio.NewWriter(tmp)
	for key, value := range params {
		if _, err := fmt.Fprintf(writer, "%s=%s\n", key, value); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", fmt.Errorf("failed to write to file: %w", err)
	}

	return tmp.Name(), nil
}

// updateMap returns the number of updates made (it operates on 1 field, so 0 or 1 only).
func updateMap(m *map[string]string, key, val string) int {
	old := (*m)[key]
	if old == val {
		return 0
	}

	(*m)[key] = val
	return 1
}

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

	paramsEnvMap, err := parseParams(paramsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// params.env doesn't exist, do not apply any changes
			return nil
		}
		return err
	}

	// will be used as a boolean (0 or non-0) and accumulate result of updates of every field
	// Could use sum, but safe from hypothetically integer overflow
	updated := 0

	// 1. Update images with env variables
	// e.g "odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	for i := range paramsEnvMap {
		relatedImageValue := os.Getenv(imageParamsMap[i])
		if relatedImageValue != "" {
			updated |= updateMap(&paramsEnvMap, i, relatedImageValue)
		}
	}

	// 2. Update other fields with extraParamsMap which are not carried from component
	for _, extraParamsMap := range extraParamsMaps {
		for eKey, eValue := range extraParamsMap {
			updated |= updateMap(&paramsEnvMap, eKey, eValue)
		}
	}

	if updated == 0 {
		return nil
	}

	tmp, err := writeParamsToTmp(paramsEnvMap, componentPath)
	if err != nil {
		return err
	}

	if err = os.Rename(tmp, paramsFile); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed rename %s to %s: %w", tmp, paramsFile, err)
	}

	return nil
}
