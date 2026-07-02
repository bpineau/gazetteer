package overview_test

import (
	"fmt"
	"log"

	"github.com/bpineau/gazetteer/overview"
)

// ExampleBuild screens every commune of two departments offline (no
// network I/O), ranking on the row's decision-grade methods rather than
// on raw fields: EffectiveRentEURM2HC already caps the market rent at
// the legal encadrement ceiling, and GrossYieldPct derives from the
// effective figures.
func ExampleBuild() {
	rows, err := overview.Build(overview.Options{Depts: []string{"93", "94"}})
	if err != nil {
		log.Fatal(err)
	}

	for _, c := range rows {
		if !c.PriceReliable() || c.GrossYieldPct() < 7 {
			continue
		}
		fmt.Printf("%s %s: %.0f €/m², %.1f €/m² HC, yield %.1f%%\n",
			c.INSEE, c.Name,
			c.EffectivePriceEURM2(), c.EffectiveRentEURM2HC(), c.GrossYieldPct())
	}
}
