package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/PuerkitoBio/goquery"
	bolt "go.etcd.io/bbolt"
)

const (
	urlTemplate      = "https://www.life-moon.pp.ru/moon-day-info/%s/%d/"
	urlDateFormat    = "2006-01-02"
	outputDateFormat = "02.01.2006"
)

var (
	httpClient = http.Client{
		Timeout: time.Second * 2, // Timeout after 2 seconds
	}
	rowRegex     = regexp.MustCompile(`начало\s*(\d+)\s*лунного\s*дня`)
	cachePath    = filepath.Join(os.Getenv("HOME"), ".cache", "moon-day", "cache")
	cacheStorage *bolt.DB
)

func getMoonDayInfo(date time.Time, cityID int64) (tableRows [][]string, err error) {
	var (
		urlDateStr    = date.Format(urlDateFormat)
		formatDateStr = date.Format(outputDateFormat)
	)

	u := fmt.Sprintf(urlTemplate, urlDateStr, cityID)
	res, err := httpClient.Get(u)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	doc.Find("table.moon-events-table > tbody > tr").Each(func(i int, s *goquery.Selection) {
		var td = s.Find("td")
		var td0 = td.Eq(0).Text()
		var td1 = td.Eq(1).Text()
		match := rowRegex.FindStringSubmatch(td1)
		if len(match) == 0 {
			return
		}

		var tableRow = []string{
			fmt.Sprintf("%s %s", formatDateStr, td0),
			match[1],
		}
		tableRows = append(tableRows, tableRow)
	})

	return tableRows, nil
}

func getMoonDayInfoCache(date time.Time, cityID int64, skipCache bool) (out [][]string, err error) {
	var cacheKey = fmt.Sprintf("%s-%d", date.Format(outputDateFormat), cityID)
	var val []byte
	var tx *bolt.Tx

	tx, err = cacheStorage.Begin(true)
	if err != nil {
		return
	}

	defer tx.Rollback()

	var b *bolt.Bucket
	b, err = tx.CreateBucketIfNotExists([]byte("cache"))
	if err != nil {
		return
	}

	if skipCache {
		goto skipCache
	}

	val = b.Get([]byte(cacheKey))
	if val == nil {
		goto skipCache
	}

	err = json.Unmarshal(val, &out)
	if err != nil {
		return
	}

	return

skipCache:
	out, err = getMoonDayInfo(date, cityID)
	if err != nil {
		return
	}

	val, err = json.Marshal(out)
	if err != nil {
		return
	}

	err = b.Put([]byte(cacheKey), val)
	if err != nil {
		return
	}

	err = tx.Commit()
	if err != nil {
		return
	}

	return
}

func getInfo(cityID, daysBefore, daysAfter int64, skipCache bool) (tableRows [][]string, err error) {
	var (
		ts   = time.Now()
		rows [][]string
	)

	for i := -daysBefore; i <= daysAfter; i++ {
		var dur = time.Duration(i) * 24 * time.Hour
		var day = ts.Add(dur)
		rows, err = getMoonDayInfoCache(day, cityID, skipCache)
		if err != nil {
			return nil, err
		}
		tableRows = append(tableRows, rows...)
	}

	return
}

func main() {
	var (
		cityID     = flag.Int64("city-id", 31, "city id")
		daysBefore = flag.Int64("days-before", 1, "days count before today")
		daysAfter  = flag.Int64("days-after", 1, "days count after today")
		skipCache  = flag.Bool("skip-cache", false, "skip cache")
		err        error
	)
	flag.Parse()

	cacheStorage, err = bolt.Open(cachePath, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer cacheStorage.Close()

	tableRows, err := getInfo(*cityID, *daysBefore, *daysAfter, *skipCache)
	if err != nil {
		log.Fatal(err)
	}

	var writer = csv.NewWriter(os.Stdout)
	writer.Comma = '\t'
	err = writer.WriteAll(tableRows)
	if err != nil {
		log.Fatal(err)
	}
}
