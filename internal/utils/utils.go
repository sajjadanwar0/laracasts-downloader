package utils

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CleanJSON cleans and validates JSON content
func CleanJSON(content string) (string, error) {
	// First try direct decoding
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(content), &jsonData); err == nil {
		cleanedJSON, _ := json.Marshal(jsonData)
		return string(cleanedJSON), nil
	}

	// Unescape content
	unescaped, err := url.QueryUnescape(content)
	if err == nil {
		content = unescaped
	}

	// Clean escape characters
	content = strings.ReplaceAll(content, `\"`, `"`)
	content = strings.ReplaceAll(content, `\\`, `\`)
	content = strings.ReplaceAll(content, `\/`, `/`)

	// Find JSON between { and }
	startIdx := strings.Index(content, "{")
	endIdx := strings.LastIndex(content, "}")

	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return "", fmt.Errorf("no valid JSON object found")
	}

	jsonStr := content[startIdx : endIdx+1]

	// Validate JSON
	var rawJSON json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(jsonStr))
	decoder.UseNumber()

	if err := decoder.Decode(&rawJSON); err != nil {
		return "", fmt.Errorf("failed to parse JSON: %v", err)
	}

	return string(rawJSON), nil
}
