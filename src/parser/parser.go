package parser

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// ParseJSON takes a JSON document as input and returns a parsed representation of the JSON data.
func ParseJSON(jsonData []byte) (map[string]interface{}, error) {
	var parsedData map[string]interface{}
	err := json.Unmarshal(jsonData, &parsedData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}
	return parsedData, nil
}

// FetchJSON fetches a JSON document from the specified URL.
func FetchJSON(url string) ([]byte, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JSON: %v", err)
	}
	defer response.Body.Close()

	jsonData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}