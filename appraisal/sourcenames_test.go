package appraisal_test

import (
	"slices"
	"testing"

	"github.com/bpineau/gazetteer/appraisal"

	// Imported for their init() registration — the names below only
	// appear once each package has registered its Result factory.
	_ "github.com/bpineau/gazetteer/sources/carteloyers"
	_ "github.com/bpineau/gazetteer/sources/dvf"
	_ "github.com/bpineau/gazetteer/sources/encadrement"
	_ "github.com/bpineau/gazetteer/sources/georisques"
	_ "github.com/bpineau/gazetteer/sources/oll"
)

func TestSourceNames(t *testing.T) {
	t.Parallel()

	price := appraisal.PriceSourceNames()
	if !slices.Contains(price, "dvf") {
		t.Errorf("PriceSourceNames = %v, want dvf included", price)
	}

	rent := appraisal.RentSourceNames()
	for _, want := range []string{"carteloyers", "encadrement", "oll"} {
		if !slices.Contains(rent, want) {
			t.Errorf("RentSourceNames = %v, want %s included", rent, want)
		}
	}
	if slices.Contains(rent, "dvf") {
		t.Errorf("RentSourceNames includes dvf, which is price-only")
	}

	hazard := appraisal.HazardSourceNames()
	if !slices.Contains(hazard, "georisques") {
		t.Errorf("HazardSourceNames = %v, want georisques included", hazard)
	}

	if !slices.IsSorted(price) || !slices.IsSorted(rent) || !slices.IsSorted(hazard) {
		t.Error("source-name lists must be sorted (registry order)")
	}
}
