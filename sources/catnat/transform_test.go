package catnat

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"
)

const sampleCSV = "cod_nat_catnat;cod_commune;lib_commune;num_risque_jo;lib_risque_jo;dat_deb;dat_fin;dat_pub_arrete;dat_pub_jo;dat_maj\r\n" +
	"A1;91471;Ville;INND;Inondations et/ou Coulées de Boue;1999-12-25;1999-12-29;2000-01-01;2000-01-05;\r\n" +
	"A2;91471;Ville;SECH;Sécheresse;2022-07-01;2022-09-30;2023-04-01;2023-04-05;\r\n" +
	"A3;91471;Ville;SECH;Sécheresse;2018-01-01;2018-12-31;2019-06-01;2019-06-05;\r\n" +
	"A4;91471;Ville;MVTT;Mouvement de Terrain;2024-05-01;2024-05-02;2024-09-01;2024-09-05;\r\n" +
	"A5;77001;Autre;TEMP;Tempête;1999-12-26;1999-12-26;2000-01-01;2000-01-05;\r\n"

func TestAggregate_Golden(t *testing.T) {
	t.Parallel()
	p, err := aggregate([]byte(sampleCSV))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if p.RefYear != 2024 {
		t.Errorf("RefYear = %d, want 2024 (latest event)", p.RefYear)
	}
	if p.WindowYears != recentWindowYears {
		t.Errorf("WindowYears = %d, want %d", p.WindowYears, recentWindowYears)
	}
	byInsee := map[string]Entry{}
	for _, c := range p.Communes {
		byInsee[c.INSEE] = c
	}
	v := byInsee["91471"]
	// 4 decrees: 1 inond + 2 sech + 1 mvt; recent window [2015..2024] → 2018,
	// 2022, 2024 = 3 (the 1999 inond is outside).
	if v.Total != 4 || v.Inond != 1 || v.Sech != 2 || v.Mvt != 1 {
		t.Errorf("91471 = %+v, want total 4 / inond 1 / sech 2 / mvt 1", v)
	}
	if v.Recent != 3 {
		t.Errorf("91471 Recent = %d, want 3 (2018/2022/2024 in window)", v.Recent)
	}
	if v.LastYear != 2024 {
		t.Errorf("91471 LastYear = %d, want 2024", v.LastYear)
	}
	// Deterministic INSEE order.
	if p.Communes[0].INSEE != "77001" || p.Communes[1].INSEE != "91471" {
		t.Errorf("communes not sorted by INSEE: %v", []string{p.Communes[0].INSEE, p.Communes[1].INSEE})
	}
}

func TestCategorize(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Inondations et/ou Coulées de Boue": "inond",
		"Inondations Remontée Nappe":        "inond",
		"Sécheresse":                        "sech",
		"Mouvement de Terrain":              "mvt",
		"Glissement de Terrain":             "mvt",
		"Tempête":                           "temp",
		"Vents Cycloniques":                 "temp",
		"Grêle":                             "",
		"Secousse Sismique":                 "",
		"Chocs Mécaniques liés à l'action des Vagues": "", // coastal — total only
		"Raz de Marée": "",
	}
	for in, want := range cases {
		if got := categorize(in); got != want {
			t.Errorf("categorize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAggregate_MissingColumn guards the column check: a header without
// cod_commune must fail the rebuild loudly, not silently read the wrong column.
func TestAggregate_MissingColumn(t *testing.T) {
	t.Parallel()
	bad := "cod_nat_catnat;lib_commune;lib_risque_jo;dat_deb\r\nA1;Ville;Sécheresse;2022-01-01\r\n"
	if _, err := aggregate([]byte(bad)); err == nil {
		t.Error("aggregate should error when cod_commune is absent")
	}
}

// TestTransform_Roundtrip exercises the full zip→gzip path.
func TestTransform_Roundtrip(t *testing.T) {
	t.Parallel()
	// Build a minimal GASPAR-like zip holding the sample CSV.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("catnat_gaspar.csv")
	_, _ = w.Write([]byte(sampleCSV))
	_ = zw.Close()

	var out bytes.Buffer
	if err := transform(context.Background(), rawSetStub{zbuf.Bytes()}, &out); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(out.Bytes())); err == nil {
		t.Error("validate should fail on a 2-commune fixture (< 10000)")
	}
	gz, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Communes) != 2 {
		t.Errorf("communes = %d, want 2", len(p.Communes))
	}
}

type rawSetStub struct{ b []byte }

func (s rawSetStub) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.b)), nil
}
