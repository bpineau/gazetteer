package qpv

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// The QPV contours name Paris, Lyon and Marseille by their PARENT code only
// (75056, 69123, 13055), and the commune index used to fold every
// arrondissement onto it: Paris 7e and 16e, which host no QPV, answered
// HasQPV true with the 21 Paris QPVs, and an overview screen excluding QPV
// communes dropped all twenty arrondissements. Membership is now resolved
// per arrondissement from the arrondissement contours at refresh time.
func TestIndex_HasQPV_PerArrondissement(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for insee, want := range map[string]bool{
		"75107": false, // Paris 7e
		"75116": false, // Paris 16e
		"75108": false, // Paris 8e
		"75118": true,  // Paris 18e (Goutte d'Or, Porte de la Chapelle...)
		"75119": true,  // Paris 19e
		"75056": true,  // the parent still answers for the whole city
		"13202": true,  // Marseille 2e
		"13208": false, // Marseille 8e
		"69123": true,  // Lyon parent
		"69389": true,  // Lyon 9e (La Duchère)
		"69382": false, // Lyon 2e
	} {
		if got := idx.HasQPV(insee); got != want {
			t.Errorf("HasQPV(%s) = %v, want %v", insee, got, want)
		}
	}

	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75107"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.HasQPV || res.MatchLevel != MatchLevelCommune {
		t.Errorf("Paris 7e, no coordinates: HasQPV=%v level=%q, want false/commune", res.HasQPV, res.MatchLevel)
	}
	res, err = Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV || res.QPVCount < 2 || res.QPVCount >= 21 {
		t.Errorf("Paris 18e: HasQPV=%v count=%d, want the 18e's own QPVs, not all of Paris's", res.HasQPV, res.QPVCount)
	}
}
