package dvfagg_test

import (
	"fmt"
	"log"

	"github.com/bpineau/gazetteer/sources/dvfagg"
)

// ExampleLoad shows the batch-read pattern: load the commune-keyed price
// index once (an empty dir means the embedded dataset), then look up as
// many communes as needed without going through the Listing/Query path.
func ExampleLoad() {
	idx, err := dvfagg.Load("")
	if err != nil {
		log.Fatal(err)
	}

	for _, insee := range idx.Codes() {
		r, ok := idx.Lookup(insee)
		if !ok {
			continue
		}
		fmt.Printf("%s: median %.0f €/m² over %d sales\n",
			insee, r.PriceMedianEURM2, r.N)
	}
}
