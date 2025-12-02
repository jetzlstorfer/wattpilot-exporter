package wattpilotutils

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const OfficialPricePerKwh2024 = 0.33182
const OfficialPricePerKwh2025 = 0.35889 // https://www.bmf.gv.at/themen/steuern/arbeitnehmerinnenveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html
const OfficialPricePerKwh2026 = 0.32806
const PurchasePricePerKwh2024 = 0.2824
const PurchasePricePerKwh2025 = 0.25
const PurchasePricePerKwh2026 = 0.25
const JSONFileName = "data.json"
const WattpilotDataUrl = "https://data.wattpilot.io/api/v1/direct_json?e=TBD&from=TBD&to=TBD&timezone=Europe%2FVienna"

type WattpilotColumn struct {
	Key  string `json:"key"`
	Hide bool   `json:"hide,omitempty"`
	Unit string `json:"unit,omitempty"`
	Type string `json:"type,omitempty"`
}

type WattpilotData struct {
	Columns []WattpilotColumn `json:"columns"`
	Data    []WattpilotEntry  `json:"data"`
}

type WattpilotEntry struct {
	SessionNumber     int     `json:"session_number"`
	SessionIdentifier string  `json:"session_identifier"`
	IDChip            string  `json:"id_chip"`
	IDChipName        string  `json:"id_chip_name"`
	Eco               float64 `json:"eco"`
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

	//log.Println("getting json data from url: ", url)

	jsonData, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}

func GetJSONData() ([]byte, error) {
	// Read JSON document from file.
	jsonData, err := readJSONFile(JSONFileName)
	if err != nil {
		log.Printf("Failed to read JSON file: %v", err)
	}

	// If JSON file not found, fetch data from the web and store in file.
	if jsonData == nil {
		log.Println("JSON file not found, fetching data from the web")
		key := os.Getenv("WATTPILOT_KEY")
		myUrl := PrepUrl(WattpilotDataUrl, "", "", key)

		// Fetch JSON document from the web.
		jsonData, err = FetchJSON(myUrl) // Remove the redeclaration of jsonData
		if err != nil || jsonData == nil {
			log.Fatalf("Failed to fetch JSON: %v", err)
		}
		err = saveJSONFile(JSONFileName, jsonData)
		if err != nil {
			log.Fatalf("Failed to save JSON file: %v", err)
		}
	}

	return jsonData, nil
}

func readJSONFile(filename string) ([]byte, error) {
	jsonFile, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSON file: %v", err)
	}
	defer jsonFile.Close()

	jsonData, err := io.ReadAll(jsonFile)
	if err != nil {
		return jsonData, fmt.Errorf("failed to read JSON data: %v", err)
	}
	return jsonData, nil
}

func saveJSONFile(filename string, jsonData []byte) error {
	jsonFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create JSON file: %v", err)
	}
	defer jsonFile.Close()

	_, err = jsonFile.Write(jsonData)
	if err != nil {
		return fmt.Errorf("failed to write JSON data: %v", err)
	}
	return nil
}

func PrepUrl(wattpilotDataUrl string, from string, to string, key string) string {
	myUrl, err := url.Parse(wattpilotDataUrl)
	if err != nil {
		log.Fatal(err)
	}
	values := myUrl.Query()
	if from == "" || to == "" {
		values.Del("from")
		values.Del("to")
	} else {
		values.Set("from", from)
		values.Set(("to"), to)
	}
	values.Set("e", key)
	myUrl.RawQuery = values.Encode()
	return myUrl.String()
}

func GetUnixTimestampStart(yearMonth string) string {
	// year-month into unix timestamp
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal(err)
	}
	t, _ := time.Parse("2006-01", yearMonth)
	return strconv.FormatInt(t.In(loc).Unix()*1000, 10)
}
func GetUnixTimestampEnd(yearMonth string) string {
	// year-month into unix timestamp
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal(err)
	}
	t, _ := time.Parse("2006-01", yearMonth)

	// add one month
	t = t.AddDate(0, 1, 0)
	// subtract one second to get last date of the month
	t = t.Add(-1 * time.Second)
	return strconv.FormatInt(t.In(loc).Unix()*1000, 10)
}

func GetPrevMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, -1, 0)
	// only allow dates after 2024-06
	if t.Before(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)) {
		return ""
	}
	return t.Format("2006-01")
}

func GetNextMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, 1, 0)
	// only allow dates before or equal to current month
	if t.After(time.Now()) {
		return ""
	}
	return t.Format("2006-01")
}

func GetStatsForMonth(monthToCalculate string) WattpilotData {
	// year-month into unix timestamp
	//from := GetUnixTimestampStart(monthToCalculate)
	//to := GetUnixTimestampEnd(monthToCalculate)
	//key := os.Getenv("WATTPILOT_KEY")
	//myUrl := PrepUrl(WattpilotDataUrl, from, to, key)

	// Fetch JSON document from the web
	jsonData, err := GetJSONData()
	if err != nil {
		log.Fatalf("Failed to fetch JSON: %v", err)
	}

	// Parse JSON document
	parsedData, err := ParseJSON(jsonData)
	if err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// now convert the data to the correct format
	monthlyData := WattpilotData{}
	monthlyData.Columns = parsedData.Columns

	newData := []WattpilotEntry{}

	for _, data := range parsedData.Data {
		// fmt 29.06.2024 21:14:32
		// https://gist.github.com/unstppbl/26942512b3ca6a92857c87124445ca0b
		month, _ := time.Parse("02.01.2006 15:04:05", data.End)
		if month.Format("2006-01") == monthToCalculate {
			newData = append(newData, data)
		}
	}

	monthlyData.Data = newData
	return monthlyData
}

func GetStatsForMonths(months []string) []WattpilotData {
	var data []WattpilotData
	for _, month := range months {
		data = append(data, GetStatsForMonth(strings.TrimSpace(month)))
	}
	return data
}

func RoundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func getSellingPriceOfYear(timestamp string) float64 {
	year, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	switch year.Year() {
	case 2024:
		return OfficialPricePerKwh2024
	case 2025:
		return OfficialPricePerKwh2025
	case 2026:
		return OfficialPricePerKwh2026
	default:
		// set default to 2025 until we have no data for other years
		return OfficialPricePerKwh2025
	}
}

func getPurchasePriceOfYear(timestamp string) float64 {
	year, _ := time.Parse("02.01.2006 15:04:05", timestamp)
	switch year.Year() {
	case 2024:
		return PurchasePricePerKwh2024
	case 2025:
		return PurchasePricePerKwh2025
	case 2026:
		return PurchasePricePerKwh2026
	default:
		// set default to 2025 until we have no data for other years
		return PurchasePricePerKwh2025
	}
}

func CalculatePrice(endTime string, energy float64, eco float64) float64 {
	return energy * getSellingPriceOfYear(endTime)
}

func CalculatePriceMargin(endTime string, energy float64, eco float64) float64 {
	if eco == 100 {
		return energy * getSellingPriceOfYear(endTime)
	} else {
		// TODO calculate the correct ratio
		return energy * (getSellingPriceOfYear(endTime) - getPurchasePriceOfYear(endTime))
	}
}

func RefreshData() {
	key := os.Getenv("WATTPILOT_KEY")
	myUrl := PrepUrl(WattpilotDataUrl, "", "", key)
	// Fetch JSON document from the web
	jsonData, err := FetchJSON(myUrl)
	if err != nil {
		log.Fatalf("Failed to fetch JSON: %v", err)
	}

	// Save JSON document to file
	err = saveJSONFile(JSONFileName, jsonData)
	if err != nil {
		log.Fatalf("Failed to save JSON file: %v", err)
	}
}
