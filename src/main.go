package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jetzlstorfer/wattpilot-exporter/parser"
	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

type Data struct {
	Date             string
	PrevMonth        string
	NextMonth        string
	ChargingSessions int
	LatestSession    string
	TotalEnergy      float64
	TotalPrice       float64
}

const wattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"

func calculateData(date string) (Data, error) {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		//log.Fatal("Error loading .env file")
		log.Println("Error loading .env file: " + err.Error())
	}

	monthToCalculate := time.Now().Format("2006-01")
	if date != "" {
		monthToCalculate = date
	}

	// year-month into unix timestamp
	from := wattpilotutils.GetUnixTimestampStart(monthToCalculate)
	to := wattpilotutils.GetUnixTimestampEnd(monthToCalculate)
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
	latestSession := ""

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		// TODO also take eco mode into consideration
		totalPrice += data.Energy * (data.Eco / 100) * wattpilotutils.OfficialPricePerKwh
		latestSession = data.Start
	}

	fmt.Println(monthToCalculate)
	fmt.Println("Total Energy in kWh:", totalEnergy)
	fmt.Println("Total Energy in €:", totalPrice)

	return Data{
		Date:             monthToCalculate,
		PrevMonth:        wattpilotutils.GetPrevMonth(monthToCalculate),
		NextMonth:        wattpilotutils.GetNextMonth(monthToCalculate),
		ChargingSessions: len(parsedData.Data),
		LatestSession:    latestSession,
		TotalEnergy:      totalEnergy,
		TotalPrice:       totalPrice}, nil

}

func handler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	data, err := calculateData(date)
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
