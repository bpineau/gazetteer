package encadrement

import "testing"

// The embedded zonage artifacts are the source of truth for EPT commune
// membership; these probes pin the accessor against known communes.
func TestZonesForINSEE(t *testing.T) {
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		insee    string
		ept      string
		minZones int
	}{
		{"93066", ZoneSourcePlaineCommune, 2}, // Saint-Denis spans two zones
		{"93001", ZoneSourcePlaineCommune, 1}, // Aubervilliers
		{"93048", ZoneSourceEstEnsemble, 2},   // Montreuil spans two zones
		{"93055", ZoneSourceEstEnsemble, 1},   // Pantin
	}
	for _, c := range cases {
		zones, ept, ok := idx.ZonesForINSEE(c.insee)
		if !ok || ept != c.ept || len(zones) < c.minZones {
			t.Errorf("ZonesForINSEE(%s) = (%v, %q, %v), want ept=%q with >=%d zones",
				c.insee, zones, ept, ok, c.ept, c.minZones)
		}
		for _, z := range zones {
			if len(idx.LookupEPTZone(ept, z)) == 0 {
				t.Errorf("LookupEPTZone(%q, %q) returned no cells for %s", ept, z, c.insee)
			}
		}
	}
	if _, _, ok := idx.ZonesForINSEE("75056"); ok {
		t.Error("ZonesForINSEE(75056) = ok, want false (Paris is arrondissement-keyed)")
	}
	var nilIdx *Index
	if _, _, ok := nilIdx.ZonesForINSEE("93001"); ok {
		t.Error("nil index must report ok=false")
	}
}
