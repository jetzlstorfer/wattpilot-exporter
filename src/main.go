package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
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
	FormattedDate    string
	PrevMonth        string
	NextMonth        string
	ChargingSessions int
	LatestSession    string
	IsCharging       bool
	TotalEnergy      float64
	TotalPrice       float64
	TotalMargin      float64
	Sessions         []SessionPoint
	Error            string
}

type ConfigData struct {
	KeySet  bool
	Message string
	Error   string
}

// requireKey redirects to /config if no API key is configured and returns false.
// Returns true when a key is available and the handler may proceed.
func requireKey(w http.ResponseWriter, r *http.Request) bool {
	if wattpilotutils.GetWattpilotKey() == "" {
		http.Redirect(w, r, "/config", http.StatusSeeOther)
		return false
	}
	return true
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("config.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("configHandler: template parse error: %v", err)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			tmpl.Execute(w, ConfigData{Error: "Failed to read form data."})
			return
		}
		key := strings.TrimSpace(r.FormValue("wattpilot_key"))
		if key == "" {
			w.WriteHeader(http.StatusBadRequest)
			tmpl.Execute(w, ConfigData{Error: "Key must not be empty."})
			return
		}
		if err := wattpilotutils.SaveAppConfig(key); err != nil {
			log.Printf("configHandler: failed to save config: %v", err)
			tmpl.Execute(w, ConfigData{Error: "Failed to save configuration. Check server logs."})
			return
		}
		log.Println("WATTPILOT_KEY saved via config page")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// GET – show current status
	cfg, err := wattpilotutils.LoadAppConfig()
	if err != nil {
		log.Printf("configHandler: failed to load config: %v", err)
	}
	tmpl.Execute(w, ConfigData{KeySet: cfg.WattpilotKey != ""})
}

func calculateData(date string) (Data, error) {

	monthToCalculate := time.Now().Format("2006-01")
	if date != "" {
		monthToCalculate = date
	}

	// Parse and format the date for display
	parsedTime, err := time.Parse("2006-01", monthToCalculate)
	if err != nil {
		log.Printf("Invalid date parameter %q: %v", monthToCalculate, err)
		return Data{
			Date:          monthToCalculate,
			FormattedDate: "Invalid date",
			PrevMonth:     "",
			NextMonth:     "",
			Error:         "The requested month is invalid. Please use format YYYY-MM.",
		}, nil
	}
	formattedDate := parsedTime.Format("January 2006")

	parsedData, err := wattpilotutils.GetStatsForMonth(monthToCalculate)

	// Return data with error message if fetch/parse failed
	if err != nil {
		log.Printf("Error fetching data: %v", err)
		return Data{
			Date:          monthToCalculate,
			FormattedDate: formattedDate,
			PrevMonth:     wattpilotutils.GetPrevMonth(monthToCalculate),
			NextMonth:     wattpilotutils.GetNextMonth(monthToCalculate),
			Error:         "Unable to fetch charging data. Please try refreshing or check back later.",
		}, nil
	}

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
		FormattedDate:    formattedDate,
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
	if !requireKey(w, r) {
		return
	}
	date := r.URL.Query().Get("date")
	data, err := calculateData(date)
	if err != nil {
		// Still show the UI even if there's an error in calculateData
		// (though calculateData now returns nil errors and embeds error message in data)
		log.Printf("mainHandler: error calculating data: %v", err)
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
	w.Header().Set("Content-Type", "image/x-icon")
	http.ServeFile(w, r, "favicon.ico")
}

func faviconSVGHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	http.ServeFile(w, r, "favicon.svg")
}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	if !requireKey(w, r) {
		return
	}
	// refresh data
	err := wattpilotutils.RefreshData()
	if err != nil {
		log.Printf("refreshHandler: failed to refresh data: %v", err)
		http.Error(w, "Failed to refresh data. Please try again later.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {

	// get env variables from .env file
	// Using Overload() instead of Load() to ensure .env always takes precedence
	// over any pre-existing environment variables (e.g. empty WATTPILOT_KEY in shell)
	err := godotenv.Overload()
	if err != nil {
		//log.Fatal("Error loading .env file")
		log.Println("Error loading .env file: " + err.Error())
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/favicon.ico", faviconHandler)
	http.HandleFunc("/favicon.svg", faviconSVGHandler)
	http.HandleFunc("/refresh", refreshHandler)
	http.HandleFunc("/charts", chartHandler)
	http.HandleFunc("/info", infoHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/config", configHandler)
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
