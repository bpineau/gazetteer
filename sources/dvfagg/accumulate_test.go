package dvfagg

import (
	"strings"
	"testing"
)

// geo-dvf repeats a local once per nature de culture of its parcel. An
// apartment published on two such rows is one apartment, and must not be
// dropped as a two-flat bundle; two flats told apart by their lot number
// still are one.
func TestAccumulate_CultureRowsAreOneLocal(t *testing.T) {
	const csv = `id_mutation,nature_mutation,type_local,valeur_fonciere,surface_reelle_bati,code_commune,code_departement,id_parcelle,nombre_pieces_principales,lot1_numero,nature_culture
m1,Vente,Appartement,300000,60,93066,93,93066000AB0001,3,12,sols
m1,Vente,Appartement,300000,60,93066,93,93066000AB0001,3,12,jardins
m2,Vente,Appartement,500000,50,93066,93,93066000AB0002,2,7,sols
m2,Vente,Appartement,500000,50,93066,93,93066000AB0002,2,8,sols
`
	m := map[string]*acc{}
	if err := accumulate(strings.NewReader(csv), m); err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	e := m["93066"]
	if e == nil || len(e.all) != 1 || e.all[0] != 5000 {
		t.Fatalf("93066 = %+v, want the one-apartment sale m1 once at 5 000 EUR/m2", e)
	}
}
