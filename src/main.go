package main

import (
	"html/template"
	"log"
	"net/http"
	"time"

	wattpilotutils "github.com/jetzlstorfer/wattpilot-exporter/utils"
	"github.com/joho/godotenv"
)

type SessionPoint struct {
	EndTime string
	Energy  float64
	Price   float64
}

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
	Sessions         []SessionPoint
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

	var sessions []SessionPoint

	// loop over the data
	for _, data := range parsedData.Data {
		totalEnergy += data.Energy
		price := wattpilotutils.CalculatePrice(data.End, data.Energy, 100)
		totalPrice += price
		totalMargin += wattpilotutils.CalculatePriceMargin(data.End, data.Energy, data.Eco)
		latestSession = data.End
		sessions = append(sessions, SessionPoint{
			EndTime: data.End,
			Energy:  wattpilotutils.RoundFloat(data.Energy, 2),
			Price:   wattpilotutils.RoundFloat(price, 2),
		})
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
		TotalMargin:      totalMargin,
		Sessions:         sessions}, nil

}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("info.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("infoHandler: template parse error: %v", err)
		return
	}
	if err := tmpl.Execute(w, nil); err != nil {
		log.Printf("infoHandler: template execute error: %v", err)
	}
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	data, err := calculateData(date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles("template.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("mainHandler: template parse error: %v", err)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("mainHandler: template execute error: %v", err)
	}
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "favicon.ico")
}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	// refresh data
	wattpilotutils.RefreshData()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {

	// get env variables from .env file
	err := godotenv.Load()
	if err != nil {
		//log.Fatal("Error loading .env file")
		log.Println("Error loading .env file: " + err.Error())
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/favicon.ico", faviconHandler)
	http.HandleFunc("/refresh", refreshHandler)
	http.HandleFunc("/charts", chartHandler)
	http.HandleFunc("/info", infoHandler)
	http.HandleFunc("/download", downloadHandler)
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
