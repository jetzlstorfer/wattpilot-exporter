package main

import (
	"html/template"
	"log"
	"net/http"
	"time"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

type Data struct {
	Date             string
	PrevMonth        string
	NextMonth        string
	ChargingSessions int
	LatestSession    string
	IsCharging       bool
	TotalEnergy      float64
	TotalPrice       float64
	TotalMargin      float64
}

func calculateData(date string) (Data, error) {

	monthToCalculate := time.Now().Format("2006-01")
	if date != "" {
		monthToCalculate = date
	}

	parsedData := wattpilotutils.GetStatsForMonth(monthToCalculate)

	// Calculate total energy & price
	totalEnergy := 0.0
	totalPrice := 0.0
	totalMargin := 0.0
	latestSession := ""

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		totalPrice += wattpilotutils.CalculatePrice(data.Energy, 100)
		// TODO also take eco mode into consideration in a correct way
		totalMargin += wattpilotutils.CalculatePriceMargin(data.Energy, data.Eco)
		latestSession = data.End
	}
	activeSession := false
	loc, _ := time.LoadLocation("Europe/Berlin")
	latestSessionTimeStamp, _ := time.Parse(time.DateTime, latestSession)
	latestSessionTimeStamp = latestSessionTimeStamp.Add(-2 * time.Hour) // fix for timezone

	if latestSessionTimeStamp.Add(1 * time.Minute).After(time.Now().In(loc)) {
		// session is active
		activeSession = true
	}

	// fmt.Println("Total Energy in kWh:", totalEnergy)
	// fmt.Println("Total Energy in €:", totalPrice)

	return Data{
		Date:             monthToCalculate,
		PrevMonth:        wattpilotutils.GetPrevMonth(monthToCalculate),
		NextMonth:        wattpilotutils.GetNextMonth(monthToCalculate),
		ChargingSessions: len(parsedData.Data),
		LatestSession:    latestSession,
		IsCharging:       activeSession,
		TotalEnergy:      totalEnergy,
		TotalPrice:       totalPrice,
		TotalMargin:      totalMargin}, nil

}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	data, err := calculateData(date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.ParseFiles("template.html"))
	tmpl.Execute(w, data)
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "favicon.ico")
}

func main() {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		//log.Fatal("Error loading .env file")
		log.Println("Error loading .env file: " + err.Error())
	}

	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/favicon.ico", faviconHandler)
	http.HandleFunc("/charts", chartHandler)
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
