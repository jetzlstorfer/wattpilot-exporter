package main

import (
	"fmt"
	"log"

	"github.com/jetzlstorfer/wattpilot-exporter/parser"
)

const url = "https://data.wattpilot.io/api/v1/direct_json?e=T_LiSArY6sNLwrqIv37GXDVuivwH5cmkLqjMK23jpROAEJAJSG3ws8x4YcErxFXXWDu-2xwOjs5ln_E&from=1722463200000&to=1725134340000&timezone=Europe%2FVienna" // Replace with the actual URL of the JSON document

func main() {

	// Fetch JSON document from the web
	jsonData, err := parser.FetchJSON(url)
	if err != nil {
		log.Fatalf("Failed to fetch JSON: %v", err)
	}

	// Parse JSON document
	parsedData, err := parser.ParseJSON(jsonData)
	if err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Print the parsed data
	fmt.Println(parsedData)
}
