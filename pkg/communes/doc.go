// Package communes ships a 35 000-row lookup table over French
// communes (INSEE code, department, lon/lat centroid, name) backed by
// an embedded snapshot of geo.api.gouv.fr.
//
// Use Default for the singleton parsed from the embedded CSV. Tests can
// supply a synthetic CSV via NewTable / ParseCSV.
//
// Neighbors uses a haversine sweep — fine for the 35 k corpus, no
// spatial index needed.
//
// Example:
//
//	tbl := communes.MustDefault()
//	c, ok := tbl.Lookup("75107")
//	if ok {
//	    fmt.Println(c.Name, c.Dept, c.Lat, c.Lon)
//	}
//	near := tbl.Neighbors("75107", 5.0) // INSEE codes within 5 km
package communes
