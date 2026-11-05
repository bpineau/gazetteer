package fraddr

import "testing"

func TestStreetOf(t *testing.T) {
	cases := []struct {
		in   string
		want Street
	}{
		{"106 Boulevard Voltaire 75011 Paris 11e Arrondissement", Street{"boulevard", "voltaire"}},
		{"106 bd Voltaire, 75011 Paris", Street{"boulevard", "voltaire"}},
		{"9 Rue Voltaire 75011 Paris", Street{"rue", "voltaire"}},
		{"12 bis av. St-Michel", Street{"avenue", "saint michel"}},
		{"12 à 18 rue Baudin", Street{"rue", "baudin"}},
		{"8 Cour des Petites-Écuries 75010 Paris", Street{"cour", "des petites ecuries"}},
		{"Residence Le Meridien, 32 rue Dareau", Street{"rue", "dareau"}},
		{"3 Impasse de Mont Louis", Street{"impasse", "de mont louis"}},
		{"Les Jardins", Street{"", "les jardins"}},
		{"", Street{}},
	}
	for _, c := range cases {
		if got := StreetOf(c.in); got != c.want {
			t.Errorf("StreetOf(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSameStreet(t *testing.T) {
	bdVoltaire := Street{"boulevard", "voltaire"}
	cases := []struct {
		a, b Street
		want bool
	}{
		{bdVoltaire, bdVoltaire, true},
		{bdVoltaire, Street{"rue", "voltaire"}, false},
		{bdVoltaire, Street{"", "voltaire"}, true},                             // type unknown on one side
		{Street{"rue", "de la paix paris"}, Street{"rue", "de la paix"}, true}, // city left after a postcode-less address
		{Street{"rue", "de la paix"}, Street{"rue", "de la paixhans"}, false},  // word-wise, not byte-wise
		{Street{"rue", ""}, Street{"rue", ""}, false},
	}
	for _, c := range cases {
		if got := SameStreet(c.a, c.b); got != c.want {
			t.Errorf("SameStreet(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
