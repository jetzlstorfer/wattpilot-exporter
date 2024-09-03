package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type WattpilotData struct {
	Columns []struct {
		Key  string `json:"key"`
		Hide bool   `json:"hide,omitempty"`
		Unit string `json:"unit,omitempty"`
		Type string `json:"type,omitempty"`
	} `json:"columns"`
	Data []struct {
		SessionNumber     int     `json:"session_number"`
		SessionIdentifier string  `json:"session_identifier"`
		IDChip            string  `json:"id_chip"`
		IDChipName        string  `json:"id_chip_name"`
		Eco               int     `json:"eco"`
		Nexttrip          int     `json:"nexttrip"`
		Start             string  `json:"start"`
		End               string  `json:"end"`
		SecondsTotal      string  `json:"seconds_total"`
		SecondsCharged    string  `json:"seconds_charged"`
		MaxPower          float64 `json:"max_power"`
		MaxCurrent        float64 `json:"max_current"`
		Energy            float64 `json:"energy"`
		EtoStart          float64 `json:"eto_start"`
		EtoEnd            float64 `json:"eto_end"`
		Link              string  `json:"link"`
	} `json:"data"`
}

// ParseJSON takes a JSON document as input and returns a parsed representation of the JSON data.
func ParseJSON(jsonData []byte) (WattpilotData, error) {
	var parsedData WattpilotData
	err := json.Unmarshal(jsonData, &parsedData)
	if err != nil {
		return parsedData, fmt.Errorf("failed to parse JSON: %v", err)
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

	jsonData, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}
