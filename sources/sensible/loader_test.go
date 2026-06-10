package sensible

import "testing"

// Every QRR in the embedded artifact must have a commune mapping (and vice
// versa): zoneCommunes is generated offline, so this trips when a refreshed
// artifact disagrees with the committed table.
func TestZoneCommunesComplete(t *testing.T) {
	idx, err := Load("-")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range idx.feats {
		if f.code == "" {
			t.Errorf("zone %q has no code", f.zone.Name)
			continue
		}
		seen[f.code] = true
		if len(zoneCommunes[f.code]) == 0 {
			t.Errorf("zone %s (%s) has no zoneCommunes entry — regenerate the table (see communes.go)", f.code, f.zone.Name)
		}
	}
	for code := range zoneCommunes {
		if !seen[code] {
			t.Errorf("zoneCommunes has stale entry %s (not in the artifact)", code)
		}
	}
	for _, c := range curatedZones {
		if len(c.INSEE) == 0 {
			t.Errorf("curated zone %q has no INSEE", c.Name)
		}
	}
}

// The commune-grain view: Sevran hosts the Beaudottes QRR; La Courneuve hosts
// both its QRR and the curated 4000; a quiet commune hosts nothing. Paris
// arrondissements fold to 75056 (Barbès QRR).
func TestZonesForCommune(t *testing.T) {
	idx, err := Load("-")
	if err != nil {
		t.Fatal(err)
	}
	if zs := idx.ZonesForCommune("93071"); len(zs) != 1 || zs[0].Kind != KindQRR {
		t.Fatalf("Sevran: got %+v", zs)
	}
	zs := idx.ZonesForCommune("93027")
	if len(zs) != 2 {
		t.Fatalf("La Courneuve should host its QRR + the curated 4000, got %+v", zs)
	}
	if zs := idx.ZonesForCommune("75118"); len(zs) == 0 {
		t.Fatalf("Paris 18e should fold to 75056 and host the Barbès QRR")
	}
	if zs := idx.ZonesForCommune("78646"); len(zs) != 0 {
		t.Fatalf("Versailles should host nothing, got %+v", zs)
	}
}
