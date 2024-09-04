package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jetzlstorfer/wattpilot-exporter/parser"
	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

const wattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"

func main() {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	firstMonthToCalculate := "2024-08"
	// currentMonth := time.Now().Format("2006-01")

	// year-month into unix timestamp
	from := wattpilotutils.GetUnixTimestampStart(firstMonthToCalculate)
	to := wattpilotutils.GetUnixTimestampEnd(firstMonthToCalculate)
	key := os.Getenv("WATTPILOT_KEY")
	myUrl := wattpilotutils.PrepUrl(wattpilotDataUrl, from, to, key)

	// fmt.Println(myUrl.String())

	// Fetch JSON document from the web
	jsonData, err := parser.FetchJSON(myUrl)
	if err != nil {
		log.Fatalf("Failed to fetch JSON: %v", err)
	}

	// Parse JSON document
	parsedData, err := parser.ParseJSON(jsonData)
	if err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Calculate total energy & price
	totalEnergy := 0.0
	totalPrice := 0.0

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		// TODO also take eco mode into consideration
		totalPrice += data.Energy * wattpilotutils.OfficialPricePerKwh
	}

	fmt.Println("Total Energy in kWh:", totalEnergy)
	fmt.Println("Total Energy in €:", totalPrice)

}
