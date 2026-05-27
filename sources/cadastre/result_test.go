package cadastre

import (
	"math"
	"testing"
)

func TestResult_IsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    *Result
		want bool
	}{
		{"nil", nil, true},
		{"no_parcels", &Result{}, true},
		{"empty_slice", &Result{Parcels: []Parcel{}}, true},
		{"one_parcel", &Result{Parcels: []Parcel{{ID: "75104000AE0003"}}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.r.IsEmpty(); got != tc.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMakeParcel_DerivedFields(t *testing.T) {
	t.Parallel()

	p := MakeParcel("75104000AE0003", "75104", "000", "AE", "0003", 15168)
	if p.ID != "75104000AE0003" {
		t.Errorf("ID = %q, want upstream idu unchanged", p.ID)
	}
	if p.ContenanceM2 != 15168 {
		t.Errorf("ContenanceM2 = %d, want 15168", p.ContenanceM2)
	}
	if math.Abs(p.ContenanceAres-151.68) > 1e-9 {
		t.Errorf("ContenanceAres = %v, want 151.68", p.ContenanceAres)
	}
	if math.Abs(p.ContenanceHa-1.5168) > 1e-9 {
		t.Errorf("ContenanceHa = %v, want 1.5168", p.ContenanceHa)
	}
	want := "https://cadastre.data.gouv.fr/map?style=ortho&parcelleId=75104000AE0003"
	if p.MapURL != want {
		t.Errorf("MapURL = %q\nwant %q", p.MapURL, want)
	}
}

func TestMakeParcel_FallbackRecomposesIDWhenIDUEmpty(t *testing.T) {
	t.Parallel()

	p := MakeParcel("", "78005", "000", "A", "0285", 432)
	if p.ID != "780050000A0285" {
		t.Errorf("ID = %q, want recomposed 780050000A0285", p.ID)
	}
	if p.MapURL == "" {
		t.Error("MapURL is empty on a recomposed id")
	}
}

func TestMakeParcel_ZeroContenance(t *testing.T) {
	t.Parallel()

	p := MakeParcel("X", "78005", "000", "A", "0285", 0)
	if p.ContenanceAres != 0 || p.ContenanceHa != 0 {
		t.Errorf("zero contenance leaks: ares=%v ha=%v", p.ContenanceAres, p.ContenanceHa)
	}
}
