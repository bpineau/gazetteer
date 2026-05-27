package locservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestURLForINSEE(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		insee     string
		logement  string
		wantSub   string
		wantErr   bool
		errTarget error
	}{
		{
			name:    "all_types",
			insee:   "75107",
			wantSub: "/tensiometre-75107.html",
		},
		{
			name:     "with_logement_T2",
			insee:    "10387",
			logement: "T2",
			wantSub:  "/tensiometre-T2-10387.html",
		},
		{
			name:     "with_logement_chambre",
			insee:    "75107",
			logement: "chambre",
			wantSub:  "/tensiometre-chambre-75107.html",
		},
		{
			name:      "empty_insee",
			insee:     "",
			wantErr:   true,
			errTarget: ErrInsufficientFilter,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := URLForINSEE(tc.insee, tc.logement)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				if tc.errTarget != nil && !errors.Is(err, tc.errTarget) {
					t.Errorf("err = %v, want wrap of %v", err, tc.errTarget)
				}
				return
			}
			if err != nil {
				t.Fatalf("URLForINSEE: %v", err)
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("URLForINSEE = %q, want substring %q", got, tc.wantSub)
			}
		})
	}
}

func TestNormalizeLogement(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":    "",
		"T2":  "T2",
		"T5+": "T5",
		"F4+": "F4",
		"F3":  "F3",
	}
	for in, want := range cases {
		if got := NormalizeLogement(in); got != want {
			t.Errorf("NormalizeLogement(%q) = %q want %q", in, got, want)
		}
	}
}

func TestMapTypeToLogement(t *testing.T) {
	t.Parallel()

	r1, r2, r3, r4, r5, r6 := 1, 2, 3, 4, 5, 6
	cases := []struct {
		name  string
		pt    gazetteer.PropertyType
		rooms *int
		want  string
	}{
		{"apartment_no_rooms", gazetteer.PropertyApartment, nil, ""},
		{"apartment_1room", gazetteer.PropertyApartment, &r1, "studio"},
		{"apartment_2rooms", gazetteer.PropertyApartment, &r2, "T2"},
		{"apartment_3rooms", gazetteer.PropertyApartment, &r3, "T3"},
		{"apartment_4rooms", gazetteer.PropertyApartment, &r4, "T4"},
		{"apartment_5rooms", gazetteer.PropertyApartment, &r5, "T5"},
		{"apartment_6rooms", gazetteer.PropertyApartment, &r6, "T5"},
		{"house_no_rooms", gazetteer.PropertyHouse, nil, "F3"},
		{"house_4rooms", gazetteer.PropertyHouse, &r4, "F3"},
		{"house_5rooms", gazetteer.PropertyHouse, &r5, "F4"},
		{"house_6rooms", gazetteer.PropertyHouse, &r6, "F4"},
		{"land", gazetteer.PropertyLand, nil, ""},
		{"commercial", gazetteer.PropertyCommercial, &r2, ""},
		{"unknown", gazetteer.PropertyUnknown, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MapTypeToLogement(tc.pt, tc.rooms); got != tc.want {
				t.Errorf("MapTypeToLogement(%v,%v) = %q want %q", tc.pt, tc.rooms, got, tc.want)
			}
		})
	}
}
