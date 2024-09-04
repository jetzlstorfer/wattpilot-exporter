package main

import (
	"fmt"
	"log"
	"os"
	"net/http"
	"html/template"

	"github.com/jetzlstorfer/wattpilot-exporter/parser"
	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

type Data struct {
	TotalEnergy float64
	TotalPrice  float64
}

const wattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"

func calculateData() (Data, error) {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		//log.Fatal("Error loading .env file")
		log.Println("Error loading .env file: " + err.Error())
	}

	firstMonthToCalculate := "2024-08"
	// currentMonth := time.Now().Format("2006-01")

	// year-month into unix timestamp
	from := wattpilotutils.GetUnixTimestampStart(firstMonthToCalculate)
	to := wattpilotutils.GetUnixTimestampEnd(firstMonthToCalculate)
	key := os.Getenv("WATTPILOT_KEY")
	myUrl := wattpilotutils.PrepUrl(wattpilotDataUrl, from, to, key)

	fmt.Println(myUrl)

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

	return Data{TotalEnergy: totalEnergy, TotalPrice: totalPrice}, nil


}

func handler(w http.ResponseWriter, r *http.Request) {
	data, err := calculateData()
	if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
	}

	tmpl := template.Must(template.ParseFiles("template.html"))
	tmpl.Execute(w, data)
}

func main() {
	http.HandleFunc("/", handler)
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}