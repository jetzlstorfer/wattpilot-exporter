package wattpilotutils

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"
	_ "time/tzdata"
)

const OfficialPricePerKwh = 0.33182

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
	} `json:"data"`
}

func PrepUrl(wattpilotDataUrl string, from string, to string, key string) string {
	myUrl, err := url.Parse(wattpilotDataUrl)
	if err != nil {
		log.Fatal(err)
	}
	values := myUrl.Query()
	values.Set("from", from)
	values.Set(("to"), to)
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

	fmt.Println(t)

	return strconv.FormatInt(t.In(loc).Unix()*1000, 10)
}

func GetPrevMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, -1, 0)
	return t.Format("2006-01")
}

func GetNextMonth(yearMonth string) string {
	t, _ := time.Parse("2006-01", yearMonth)
	t = t.AddDate(0, 1, 0)
	return t.Format("2006-01")
}
