package zonescore

// This package imports concrete sources/* (which themselves import appraisal).
// That is cycle-free ONLY because appraisal does NOT import this package — keep
// it that way: never re-export zonescore from appraisal.
import (
	"fmt"
	"sort"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/chomage"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/filoiris"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/locservice"
	"github.com/bpineau/gazetteer/sources/logiris"
	"github.com/bpineau/gazetteer/sources/nuisances"
	gzosm "github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
)

// scoreRendement — the dominant axis: gross yield from the consolidated price
// and rent. yield% = rent€/m²/month × 12 / price€/m² × 100.
//
// The 3 %→0 / 8 %→100 band encodes the yield-first thesis: below ~3 % gross a
// French rental is a capital play, not income (often negative cash-flow after
// costs); 8 %+ is the genuine high-yield secondary-market territory the profile
// rewards. The band spreads the common 4–6 % zone across the middle of the scale
// where it discriminates best.
func scoreRendement(d gazetteer.Dossier) axisResult {
	price := appraisal.PricePerM2(d)
	rent := appraisal.RentValue(d)
	if price.EurPerM2Cents <= 0 || rent.EurPerM2Cents <= 0 {
		return axisResult{}
	}
	p := float64(price.EurPerM2Cents) / 100 // €/m²
	r := float64(rent.EurPerM2Cents) / 100  // €/m²/month
	yield := r * 12 / p * 100
	return axisResult{
		value:   lerp(yield, 3, 8),
		reason:  fmt.Sprintf("rendement brut %.1f%% (loyer %.0f€/m²/mois, prix %.0f€/m²)", yield, r, p),
		sources: priceRentSources(price, rent),
		present: true,
	}
}

// priceRentSources collects the non-excluded contributors behind the yield.
func priceRentSources(p appraisal.PriceConsolidated, r appraisal.RentConsolidated) []string {
	set := map[string]struct{}{}
	for _, in := range p.Inputs {
		if !in.Excluded {
			set[in.Source] = struct{}{}
		}
	}
	for _, in := range r.Inputs {
		if !in.Excluded {
			set[in.Source] = struct{}{}
		}
	}
	return sortedKeys(set)
}

// scoreTension — lettability: rental tension (locservice), the inverse of the
// vacancy rate and the rental-market depth. Vacancy + renter share come from
// the IRIS-level census housing (logiris) when available, else the
// commune-level vacancy (vacance).
func scoreTension(d gazetteer.Dossier) axisResult {
	var subs []*float64
	var srcs []string
	reason := "demande locative"
	if ls, ok := gazetteer.Get[*locservice.Result](d, locservice.Name); ok && !ls.IsEmpty() {
		if v, ok := tensionLabelScore(ls.TensionLabel); ok {
			subs = append(subs, new(v))
			srcs = append(srcs, locservice.Name)
			reason += fmt.Sprintf(" %s", ls.TensionLabel)
		}
	}
	// Vacancy + rental-market depth. Prefer the IRIS-level census housing
	// reading (logiris) — sharper where neighbourhoods diverge within a
	// commune — over the commune-level vacancy (vacance). A low vacancy AND
	// a high renter share both mark a deep, tight rental market.
	if li, ok := gazetteer.Get[*logiris.Result](d, logiris.Name); ok && !li.IsEmpty() {
		// Combine vacancy + rental-market depth into ONE subscore, so logiris
		// and the commune fallback each weigh the same in the axis mean.
		housing := []*float64{new(lerp(li.VacancyRatePct, 12, 0))} // low vacancy → high
		if li.RenterSharePct > 0 {                                 // 0 = suppressed; skip
			housing = append(housing, new(lerp(li.RenterSharePct, 15, 70))) // deep rental market → high
		}
		if hv, ok := mean(housing...); ok {
			subs = append(subs, new(hv))
			srcs = append(srcs, logiris.Name)
			reason += fmt.Sprintf(", vacance IRIS %.1f%%, locataires %.0f%%", li.VacancyRatePct, li.RenterSharePct)
		}
	} else if vl, ok := gazetteer.Get[*vacance.Result](d, vacance.Name); ok && !vl.IsEmpty() {
		subs = append(subs, new(lerp(vl.VacancyRate, 12, 0))) // low vacancy → high score
		srcs = append(srcs, vacance.Name)
		reason += fmt.Sprintf(", vacance %.1f%%", vl.VacancyRate)
	}
	v, ok := mean(subs...)
	if !ok {
		return axisResult{}
	}
	return axisResult{value: v, reason: reason, sources: srcs, present: true}
}

func tensionLabelScore(l string) (float64, bool) {
	switch locservice.TensionLabel(l) {
	case locservice.LabelTresTendu:
		return 95, true
	case locservice.LabelTendu:
		return 75, true
	case locservice.LabelEquilibre:
		return 50, true
	case locservice.LabelDetendu:
		return 30, true
	case locservice.LabelTresDetendu:
		return 10, true
	default:
		return 0, false
	}
}

// scoreSolvabilite — tenant reliability: median income + the social-distress
// flag (filoiris at IRIS level, else filosofi at commune level) and the
// unemployment gap vs national (chomage).
func scoreSolvabilite(d gazetteer.Dossier) axisResult {
	var subs []*float64
	var srcs []string
	reason := "solvabilité locataire"
	if v, src, median, ok := incomeSubscore(d); ok {
		subs = append(subs, new(v))
		srcs = append(srcs, src)
		reason += fmt.Sprintf(", revenu médian %d€ (%s)", median, src)
	}
	if ch, ok := gazetteer.Get[*chomage.Result](d, chomage.Name); ok && !ch.IsEmpty() {
		// Below-national unemployment scores high; above scores low.
		subs = append(subs, new(lerp(ch.DeltaVsNationalPP, 4, -4)))
		srcs = append(srcs, chomage.Name)
		reason += fmt.Sprintf(", chômage %+.1f pt vs national", ch.DeltaVsNationalPP)
	}
	v, ok := mean(subs...)
	if !ok {
		return axisResult{}
	}
	return axisResult{value: v, reason: reason, sources: srcs, present: true}
}

// incomeSubscore returns the income component of solvabilité (0..100), the
// source it came from and the median used. It prefers the IRIS-level
// Filosofi reading (filoiris) — sharper where intra-commune income varies
// most, exactly the dense IDF zones — and falls back to the commune-level
// reading (filosofi). Both map a risk flag + the median (lerp 14k→30k €)
// to a score and average them. ok is false when neither source is present.
func incomeSubscore(d gazetteer.Dossier) (score float64, source string, median int, ok bool) {
	if fi, ok := gazetteer.Get[*filoiris.Result](d, filoiris.Name); ok && !fi.IsEmpty() {
		if v, ok := incomeScore(string(fi.Flag), fi.MedianEUR); ok {
			return v, filoiris.Name, fi.MedianEUR, true
		}
	}
	if fi, ok := gazetteer.Get[*filosofi.Result](d, filosofi.Name); ok && !fi.IsEmpty() {
		if v, ok := incomeScore(string(fi.Flag), fi.MedianEUR); ok {
			return v, filosofi.Name, fi.MedianEUR, true
		}
	}
	return 0, "", 0, false
}

// incomeScore maps a Filosofi risk flag + median revenu disponible to a
// 0..100 score, averaging whichever signals are present. ok is false only
// when no signal is present; incomeSubscore's callers gate on IsEmpty
// (median > 0) first, so in practice the median lerp is always present.
func incomeScore(flag string, medianEUR int) (float64, bool) {
	var fs []*float64
	if s, ok := riskFlagScore(flag); ok {
		fs = append(fs, new(s))
	}
	if medianEUR > 0 {
		fs = append(fs, new(lerp(float64(medianEUR), 14000, 30000)))
	}
	return mean(fs...)
}

// scoreSecurite — safety from the SSMSI social-distress flag (delinquance).
func scoreSecurite(d gazetteer.Dossier) axisResult {
	dl, ok := gazetteer.Get[*delinquance.Result](d, delinquance.Name)
	if !ok || dl.IsEmpty() {
		return axisResult{}
	}
	s, ok := riskFlagScore(string(dl.Flag))
	if !ok {
		return axisResult{}
	}
	return axisResult{value: s, reason: fmt.Sprintf("niveau de délinquance: %s", dl.Flag), sources: []string{delinquance.Name}, present: true}
}

// riskFlagScore maps the shared low/medium/high distress flag to a score
// (low distress → high score). Unknown/empty → not scorable.
func riskFlagScore(flag string) (float64, bool) {
	switch flag {
	case "low":
		return 85, true
	case "medium":
		return 55, true
	case "high":
		return 25, true
	default:
		return 0, false
	}
}

// scoreFiscalite — net-yield drag: the communal TFPB rate (lower → higher
// score). The rate is a percentage (e.g. 35.5). A zero rate means no voted rate
// is available (a V1-fallback taxefonciere result), so the axis is simply not
// scorable then — not a 0 % tax.
func scoreFiscalite(d gazetteer.Dossier) axisResult {
	tf, ok := gazetteer.Get[*taxefonciere.Result](d, taxefonciere.Name)
	if !ok || tf.TauxTFPBApplied <= 0 {
		return axisResult{}
	}
	return axisResult{
		value:   lerp(tf.TauxTFPBApplied, 55, 15), // 55 %→0, 15 %→100
		reason:  fmt.Sprintf("taux TFPB commune %.1f%%", tf.TauxTFPBApplied),
		sources: []string{taxefonciere.Name},
		present: true,
	}
}

// scoreAcces — access + livability: walk time to transit (osm) and the inverse
// of the cumulative-nuisance tier (nuisances).
func scoreAcces(d gazetteer.Dossier) axisResult {
	var subs []*float64
	var srcs []string
	reason := "accès + cadre de vie"
	if tr, ok := gazetteer.Get[*gzosm.Result](d, gzosm.Name); ok && tr.NearestTransitWalkMin > 0 {
		subs = append(subs, new(lerp(float64(tr.NearestTransitWalkMin), 25, 5))) // ≤5 min→100
		srcs = append(srcs, gzosm.Name)
		reason += fmt.Sprintf(", %d min à pied du transport", tr.NearestTransitWalkMin)
	}
	if nu, ok := gazetteer.Get[*nuisances.Result](d, nuisances.Name); ok && nu.Tier != "" {
		if s, ok := nuisanceTierScore(nu.Tier); ok {
			subs = append(subs, new(s))
			srcs = append(srcs, nuisances.Name)
			reason += fmt.Sprintf(", nuisances %s", nu.Tier)
		}
	}
	v, ok := mean(subs...)
	if !ok {
		return axisResult{}
	}
	return axisResult{value: v, reason: reason, sources: srcs, present: true}
}

func nuisanceTierScore(t string) (float64, bool) {
	switch t {
	case nuisances.TierCalme:
		return 90, true
	case nuisances.TierModere:
		return 65, true
	case nuisances.TierExpose:
		return 40, true
	case nuisances.TierTresExpose:
		return 15, true
	default:
		return 0, false
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
