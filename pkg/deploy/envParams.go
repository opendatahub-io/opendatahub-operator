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
		key, value, found := strings.Cut(line, "=")
		if found {
			paramsEnvMap[key] = value
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
func ApplyParams(componentPath string, file string, imageParamsMap map[string]string, extraParamsMaps ...map[string]string) error {
	paramsFile := filepath.Join(componentPath, file)
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

/*
ApplyParamsWithFallback applies params.env with a uniform fallback mechanism:
 1. First tries: <componentPath>/overlays/<overlayName>/params.env
 2. Falls back to: <componentPath>/base/params.env

Returns the path to the params.env file that was used (empty if neither exists).
*/
func ApplyParamsWithFallback(componentPath string, overlayName string, imageParamsMap map[string]string, extraParamsMaps ...map[string]string,
) (string, error) {
	// Platform-specific overlay
	overlayPath := filepath.Join(componentPath, "overlays", overlayName)
	overlayParamsFile := filepath.Join(overlayPath, "params.env")

	if _, err := os.Stat(overlayParamsFile); err == nil {
		// Overlay params.env exists, use it
		if err := ApplyParams(overlayPath, "params.env", imageParamsMap, extraParamsMaps...); err != nil {
			return "", fmt.Errorf("failed to apply overlay params from %s: %w", overlayParamsFile, err)
		}
		return overlayParamsFile, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to check overlay params file %s: %w", overlayParamsFile, err)
	}

	// Fallback to base
	basePath := filepath.Join(componentPath, "base")
	baseParamsFile := filepath.Join(basePath, "params.env")

	if _, err := os.Stat(baseParamsFile); err == nil {
		// Base params.env exists, use it as fallback
		if err := ApplyParams(basePath, "params.env", imageParamsMap, extraParamsMaps...); err != nil {
			return "", fmt.Errorf("failed to apply base params from %s: %w", baseParamsFile, err)
		}
		return baseParamsFile, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to check base params file %s: %w", baseParamsFile, err)
	}

	return "", fmt.Errorf("params.env not found: checked overlay=%s, base=%s", overlayParamsFile, baseParamsFile)
}
