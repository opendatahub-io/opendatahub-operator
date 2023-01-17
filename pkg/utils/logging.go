package utils

import (
	"encoding/json"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var prettylog = log.Log

// PrettyPrint returns a pretty format output of any value.
func PrettyPrint(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	valueJson, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		prettylog.Error(err, "Failed to marshal value")
		return fmt.Sprintf("%+v", value)
	}
	return string(valueJson)
}
