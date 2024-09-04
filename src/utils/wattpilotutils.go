package wattpilotutils

import (
	"log"
	"net/url"
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
