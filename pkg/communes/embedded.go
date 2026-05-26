package communes

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

//go:embed data/france.csv
var franceCSV []byte

// FranceCSVBytes returns the raw embedded CSV. Useful for tools that
// want to inspect the source.
func FranceCSVBytes() []byte { return franceCSV }

var (
	defaultOnce  sync.Once
	defaultTable *Table
	defaultErr   error
)

// Default returns the singleton Table parsed from the embedded CSV.
// Subsequent calls return the cached instance. Panics-free: the parse
// error is reported via the returned error.
func Default() (*Table, error) {
	defaultOnce.Do(func() {
		defaultTable, defaultErr = ParseCSV(bytes.NewReader(franceCSV))
	})
	return defaultTable, defaultErr
}

// MustDefault is the version that panics on error — handy from init() of
// downstream packages and from main programs.
func MustDefault() *Table {
	t, err := Default()
	if err != nil {
		panic("communes: load embedded: " + err.Error())
	}
	return t
}

// ParseCSV reads the france-shaped CSV (header `insee,dept,lon,lat,name`)
// and returns a Table. The first line MUST be the header.
func ParseCSV(r io.Reader) (*Table, error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	first, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("communes: read header: %w", err)
	}
	if len(first) < 5 || first[0] != "insee" {
		return nil, fmt.Errorf("communes: unexpected header %v", first)
	}
	rows := make([]Commune, 0, 35000)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("communes: parse row: %w", err)
		}
		if len(rec) < 5 {
			continue
		}
		lon, err := strconv.ParseFloat(rec[2], 64)
		if err != nil {
			continue
		}
		lat, err := strconv.ParseFloat(rec[3], 64)
		if err != nil {
			continue
		}
		rows = append(rows, Commune{
			INSEE: rec[0],
			Dept:  rec[1],
			Lon:   lon,
			Lat:   lat,
			Name:  rec[4],
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("communes: empty CSV")
	}
	return NewTable(rows), nil
}
