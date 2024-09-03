package main

import (
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/jetzlstorfer/wattpilot-exporter/parser"
	"github.com/joho/godotenv"
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

const wattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna" // Replace with the actual URL of the JSON document

func main() {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	myUrl, err := url.Parse(wattpilotDataUrl)
	if err != nil {
		log.Fatal(err)
	}
	values := myUrl.Query()
	values.Set("from", "1725141600000")
	values.Set(("to"), "1727726340000")
	values.Set("e", os.Getenv("WATTPILOT_KEY"))
	myUrl.RawQuery = values.Encode()

	// fmt.Println(myUrl.String())

	// Fetch JSON document from the web
	jsonData, err := parser.FetchJSON(myUrl.String())
	if err != nil {
		log.Fatalf("Failed to fetch JSON: %v", err)
	}

	// Parse JSON document
	parsedData, err := parser.ParseJSON(jsonData)
	if err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Calculate total energy
	totalEnergy := 0.0

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		// fmt.Println(data.Energy)
	}

	fmt.Println("Total Energy in kWh:", totalEnergy)
	fmt.Println("Total Energy in €:", totalEnergy*0.33182)

}
