package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/ademe"
	"github.com/bpineau/gazetteer/sources/anct"
	"github.com/bpineau/gazetteer/sources/bdnb"
	"github.com/bpineau/gazetteer/sources/bpe"
	"github.com/bpineau/gazetteer/sources/cadastre"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/cartofriches"
	"github.com/bpineau/gazetteer/sources/catnat"
	"github.com/bpineau/gazetteer/sources/cdsr"
	"github.com/bpineau/gazetteer/sources/chomage"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dpedist"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/education"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filoiris"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/georisques"
	ipsecoles "github.com/bpineau/gazetteer/sources/ips_ecoles"
	"github.com/bpineau/gazetteer/sources/iris"
	"github.com/bpineau/gazetteer/sources/locservice"
	"github.com/bpineau/gazetteer/sources/lovac"
	"github.com/bpineau/gazetteer/sources/nuisances"
	"github.com/bpineau/gazetteer/sources/oll"
	gzosm "github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/rpls"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

// summariseResult turns a Result envelope into (headline, extra)
// lines suitable for printDossierSummary. Successful (OK / OKEmpty)
// payloads run through the per-source renderer registered in
// sourceRenderers; failures and skips fall back to the wrapped error
// message (one line, truncated to keep the layout under a sane
// terminal width).
func summariseResult(name string, r gazetteer.Result) (string, []string) {
	switch r.Status {
	case gazetteer.StatusOK, gazetteer.StatusOKEmpty, "":
		if rdr, ok := sourceRenderers[name]; ok && r.Data != nil {
			return rdr(r.Data)
		}
		if r.Status == gazetteer.StatusOKEmpty {
			return "no data", nil
		}
		return "ok", nil
	default:
		if r.Err != nil {
			return truncate(unwrap(r.Err.Error()), 160), nil
		}
		return string(r.Status), nil
	}
}

// abbreviateStatus collapses the raw Status to a short, fixed-width
// tag so the listing column aligns regardless of which sources hit.
func abbreviateStatus(s gazetteer.Status) string {
	switch s {
	case gazetteer.StatusOK, "":
		return "ok"
	case gazetteer.StatusOKEmpty:
		return "empty"
	case gazetteer.StatusSkippedPrereq:
		return "skipped"
	case gazetteer.StatusFailedTransient:
		return "transient"
	case gazetteer.StatusFailedAntiBot:
		return "antibot"
	case gazetteer.StatusFailedOutdated:
		return "outdated"
	case gazetteer.StatusFailedPermanent:
		return "permanent"
	default:
		return string(s)
	}
}

// unwrap strips noisy framing from a typical wrapped error so the
// CLI's status column stays readable. The first colon-segment is
// usually the most informative.
func unwrap(s string) string {
	// Trim repeated package prefixes like "dvf: gazetteer: ...".
	if i := strings.Index(s, ": "); i > 0 && i < 40 {
		return s[i+2:]
	}
	return s
}

// truncate cuts s to at most max runes and appends an ellipsis when
// truncated. Operates on bytes (good enough for ASCII status text).
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// sourceRenderer projects a Source's typed payload into a compact
// human-readable summary. The first return value is a one-line
// headline that appears on the same line as `name vN status`; the
// second is an optional slice of extra detail lines, indented under
// the header by printDossierSummary.
//
// Renderers are registered per Source.Name (the registry key). A
// missing renderer falls back to printing just the Status.
type sourceRenderer func(data any) (headline string, extra []string)

var sourceRenderers = map[string]sourceRenderer{
	ademe.Name:        renderAdeme,
	anct.Name:         renderAnct,
	bdnb.Name:         renderBDNB,
	bpe.Name:          renderBPE,
	cadastre.Name:     renderCadastre,
	carteloyers.Name:  renderCarteloyers,
	cartofriches.Name: renderCartofriches,
	catnat.Name:       renderCatnat,
	cdsr.Name:         renderCDSR,
	chomage.Name:      renderChomage,
	delinquance.Name:  renderDelinquance,
	dpedist.Name:      renderDPEDist,
	dvf.Name:          renderDVF,
	education.Name:    renderEducation,
	encadrement.Name:  renderEncadrement,
	filoiris.Name:     renderFiloIris,
	filosofi.Name:     renderFilosofi,
	georisques.Name:   renderGeorisques,
	iris.Name:         renderIRIS,
	ipsecoles.Name:    renderIPSEcoles,
	locservice.Name:   renderLocservice,
	lovac.Name:        renderLovac,
	nuisances.Name:    renderNuisances,
	oll.Name:          renderOLL,
	gzosm.Name:        renderOSM,
	qpv.Name:          renderQPV,
	rpls.Name:         renderRPLS,
	taxefonciere.Name: renderTaxeFonciere,
	vacance.Name:      renderVacance,
	zonageabc.Name:    renderZonageABC,
	zonetendue.Name:   renderZoneTendue,
}

func renderDVF(data any) (string, []string) {
	r, ok := data.(*dvf.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no comparable transactions", nil
	}
	parts := []string{}
	if r.ValueEURPerM2Cents != nil {
		parts = append(parts, fmt.Sprintf("%.0f €/m²", float64(*r.ValueEURPerM2Cents)/100))
	}
	if r.SampleSize > 0 {
		parts = append(parts, fmt.Sprintf("%d sales", r.SampleSize))
	}
	if r.Evidence.LevelUsed != "" {
		parts = append(parts, "tier="+r.Evidence.LevelUsed)
	}
	if r.Confidence != "" {
		parts = append(parts, "conf="+r.Confidence)
	}
	return strings.Join(parts, ", "), nil
}

func renderAdeme(data any) (string, []string) {
	r, ok := data.(*ademe.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no DPE found", nil
	}
	parts := []string{}
	if r.DPE != nil {
		if r.DPE.EtiquetteDPE != "" {
			parts = append(parts, "DPE "+r.DPE.EtiquetteDPE)
		}
		if r.DPE.EtiquetteGES != "" {
			parts = append(parts, "GES "+r.DPE.EtiquetteGES)
		}
	}
	if r.Logement != nil {
		if r.Logement.SurfaceHabitableM2 != nil {
			parts = append(parts, fmt.Sprintf("%.0f m²", *r.Logement.SurfaceHabitableM2))
		}
		if r.Logement.AnneeConstruction != nil {
			parts = append(parts, fmt.Sprintf("built %d", *r.Logement.AnneeConstruction))
		}
		if r.Logement.TypeBatiment != "" {
			parts = append(parts, r.Logement.TypeBatiment)
		}
	}
	if r.Confidence != "" {
		parts = append(parts, "conf="+r.Confidence)
	}
	return strings.Join(parts, ", "), nil
}

func renderBDNB(data any) (string, []string) {
	r, ok := data.(*bdnb.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no building data", nil
	}
	parts := []string{}
	if r.Building != nil {
		if r.Building.AnneeConstruction != nil {
			parts = append(parts, fmt.Sprintf("built %d", *r.Building.AnneeConstruction))
		}
		if r.Building.NbLog != nil {
			parts = append(parts, fmt.Sprintf("%d dwellings", *r.Building.NbLog))
		}
		if r.Building.UsagePrincipal != "" {
			parts = append(parts, r.Building.UsagePrincipal)
		}
	}
	if r.DPE != nil && r.DPE.ClasseBilan != "" {
		parts = append(parts, "DPE bilan "+r.DPE.ClasseBilan)
	}
	return strings.Join(parts, ", "), nil
}

func renderCarteloyers(data any) (string, []string) {
	r, ok := data.(*carteloyers.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no rent reading", nil
	}
	headline := fmt.Sprintf("%.2f €/m²/mois CC (IC %.2f-%.2f, %d obs, %s)",
		r.LoyerMedEURPerM2CC,
		r.LoyerLowEURPerM2CC, r.LoyerHighEURPerM2CC,
		r.NbObservations, r.Confidence)
	if r.Typology != "" {
		headline += ", " + string(r.Typology)
	}
	return headline, nil
}

func renderEncadrement(data any) (string, []string) {
	r, ok := data.(*encadrement.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "outside any encadrement zone", nil
	}
	headline := fmt.Sprintf("loyer ref %.2f €/m²/mois HC, majoré %.2f (zone %s)",
		r.LoyerRefEURPerM2HC, r.LoyerRefMajEURPerM2HC, r.Zone)
	return headline, nil
}

func renderLocservice(data any) (string, []string) {
	r, ok := data.(*locservice.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no tension reading", nil
	}
	headline := "tension: " + r.TensionLabel
	if r.TensionScore != nil {
		headline += fmt.Sprintf(" (supply tightness %d/8", *r.TensionScore)
		if r.BudgetScore != nil {
			headline += fmt.Sprintf(", tenant budget %d/8", *r.BudgetScore)
		}
		headline += ")"
	}
	return headline, nil
}

func renderFiloIris(data any) (string, []string) {
	r, ok := data.(*filoiris.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no IRIS-level income (outside the ≥5000-hab perimeter)", nil
	}
	parts := []string{fmt.Sprintf("revenu médian IRIS %d €/an", r.MedianEUR)}
	if r.PovertyRatePct > 0 {
		parts = append(parts, fmt.Sprintf("pauvreté %.1f%%", r.PovertyRatePct))
	}
	if r.Gini > 0 {
		parts = append(parts, fmt.Sprintf("Gini %.2f", r.Gini))
	}
	if r.Flag != "" {
		parts = append(parts, "risk="+string(r.Flag))
	}
	return strings.Join(parts, ", "), nil
}

func renderFilosofi(data any) (string, []string) {
	r, ok := data.(*filosofi.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from Filosofi", nil
	}
	parts := []string{}
	if r.MedianEUR > 0 {
		parts = append(parts, fmt.Sprintf("median revenu %d €/an", r.MedianEUR))
	}
	if r.MinimaPct > 0 {
		parts = append(parts, fmt.Sprintf("minima %.1f%%", r.MinimaPct))
	}
	if r.Flag != "" {
		parts = append(parts, "risk="+string(r.Flag))
	}
	return strings.Join(parts, ", "), nil
}

func renderLovac(data any) (string, []string) {
	r, ok := data.(*lovac.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from LOVAC", nil
	}
	headline := fmt.Sprintf("vacance %.1f%%", r.VacancePct)
	if r.VacanceLongPct > 0 {
		headline += fmt.Sprintf(" (long-term %.1f%%)", r.VacanceLongPct)
	}
	return headline, nil
}

func renderTaxeFonciere(data any) (string, []string) {
	r, ok := data.(*taxefonciere.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no estimate", nil
	}
	headline := fmt.Sprintf("TF estimée %.0f €/an", r.EstimatedEURPerYear)
	parts := []string{}
	if r.TauxTFPBApplied > 0 {
		parts = append(parts, fmt.Sprintf("TFPB %.2f%%", r.TauxTFPBApplied))
	}
	if r.TauxTEOMApplied > 0 {
		parts = append(parts, fmt.Sprintf("TEOM %.2f%%", r.TauxTEOMApplied))
	}
	if len(parts) > 0 {
		headline += " (" + strings.Join(parts, " + ") + ")"
	}
	if r.TEOMEURPerYear > 0 {
		headline += fmt.Sprintf(", %.0f €/an récupérables", r.TEOMEURPerYear)
	}
	return headline, nil
}

func renderAnct(data any) (string, []string) {
	r, ok := data.(*anct.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no ANCT programme", nil
	}
	parts := []string{}
	if r.ACV {
		parts = append(parts, "Action Cœur de Ville")
	}
	if r.PVD {
		parts = append(parts, "Petites Villes de Demain")
	}
	if r.ORT {
		tag := "ORT"
		if r.DenormandieEligible {
			tag += " (Denormandie eligible)"
		}
		parts = append(parts, tag)
	}
	return strings.Join(parts, ", "), nil
}

func renderCartofriches(data any) (string, []string) {
	r, ok := data.(*cartofriches.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no referenced sites", nil
	}
	headline := fmt.Sprintf("%d sites", r.SiteCount)
	if r.TotalSurfaceM2 > 0 {
		headline += fmt.Sprintf(" (%d m²)", r.TotalSurfaceM2)
	}
	extra := []string{}
	if len(r.ByType) > 0 {
		extra = append(extra, "by type: "+formatMapCounts(r.ByType))
	}
	if len(r.ByStatus) > 0 {
		extra = append(extra, "by status: "+formatMapCounts(r.ByStatus))
	}
	return headline, extra
}

func renderBPE(data any) (string, []string) {
	r, ok := data.(*bpe.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no facilities indexed", nil
	}
	headline := fmt.Sprintf("%d facilities in curated subset", r.TotalFacilities)
	// Stable label order from AllBuckets so the CLI output is reproducible.
	parts := []string{}
	for _, b := range bpe.AllBuckets {
		if n := r.Get(b); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, b))
		}
	}
	var extra []string
	if len(parts) > 0 {
		extra = append(extra, "by bucket: "+strings.Join(parts, ", "))
	}
	return headline, extra
}

func renderChomage(data any) (string, []string) {
	r, ok := data.(*chomage.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no unemployment reading", nil
	}
	headline := fmt.Sprintf("chômage %.1f%% en ZE %s (%s, national %.1f%%, écart %+.1f pp, %s)",
		r.RatePct, r.ZECode, r.ZELabel, r.NationalRatePct, r.DeltaVsNationalPP, r.Tension)
	return headline, nil
}

func renderDPEDist(data any) (string, []string) {
	r, ok := data.(*dpedist.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no DPE in this commune", nil
	}
	headline := fmt.Sprintf("%d DPE issued (F+G %.1f%%, A+B %.1f%%, conf %s)",
		r.NbTotal, r.PassoireSharePct, r.EfficientSharePct, r.Confidence)
	parts := []string{}
	for _, l := range dpedist.AllLabels {
		if n := r.Get(l); n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d (%.1f%%)", l, n, r.Share(l)))
		}
	}
	var extra []string
	if len(parts) > 0 {
		extra = append(extra, "by class: "+strings.Join(parts, ", "))
	}
	return headline, extra
}

func renderDelinquance(data any) (string, []string) {
	r, ok := data.(*delinquance.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from SSMSI dataset", nil
	}
	parts := []string{}
	if r.Population > 0 {
		parts = append(parts, fmt.Sprintf("pop %d", r.Population))
	}
	if r.Flag != "" {
		parts = append(parts, "risk="+string(r.Flag))
	}
	headline := strings.Join(parts, ", ")
	var extra []string
	if len(r.Rates) > 0 {
		// Stable order, top-3 by rate.
		keys := make([]string, 0, len(r.Rates))
		for k := range r.Rates {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return r.Rates[keys[i]] > r.Rates[keys[j]] })
		top := keys
		if len(top) > 4 {
			top = top[:4]
		}
		buf := []string{}
		for _, k := range top {
			buf = append(buf, fmt.Sprintf("%s %.1f", k, r.Rates[k]))
		}
		extra = append(extra, "top rates /1000: "+strings.Join(buf, ", "))
	}
	return headline, extra
}

func renderEducation(data any) (string, []string) {
	r, ok := data.(*education.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no schools registered", nil
	}
	parts := []string{fmt.Sprintf("%d schools", r.NbTotal)}
	buckets := []struct {
		label string
		n     int
	}{
		{"école", r.NbEcole}, {"collège", r.NbCollege}, {"lycée", r.NbLycee},
		{"médico-social", r.NbMedicoSocial}, {"autre", r.NbOther},
	}
	mix := []string{}
	for _, b := range buckets {
		if b.n > 0 {
			mix = append(mix, fmt.Sprintf("%d %s", b.n, b.label))
		}
	}
	if len(mix) > 0 {
		parts = append(parts, "("+strings.Join(mix, ", ")+")")
	}
	return strings.Join(parts, " "), nil
}

func renderQPV(data any) (string, []string) {
	r, ok := data.(*qpv.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no QPV in this commune", nil
	}
	headline := fmt.Sprintf("%d QPV in commune", r.QPVCount)
	extra := make([]string, 0, len(r.QPVs))
	for _, q := range r.QPVs {
		extra = append(extra, fmt.Sprintf("%s — %s", q.Code, q.Label))
	}
	return headline, extra
}

func renderZonageABC(data any) (string, []string) {
	r, ok := data.(*zonageabc.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from zonage ABC dataset", nil
	}
	return fmt.Sprintf("zone %s (tension %d/4)", r.Zone, r.TensionScore), nil
}

func renderZoneTendue(data any) (string, []string) {
	r, ok := data.(*zonetendue.Result)
	if !ok || r == nil {
		return "no classification", nil
	}
	parts := []string{"tier=" + string(r.Tier)}
	if r.FlaggedTLV2013 {
		parts = append(parts, "TLV-2013")
	}
	return strings.Join(parts, ", "), nil
}

func renderOSM(data any) (string, []string) {
	r, ok := data.(*gzosm.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no transit in range", nil
	}
	headline := fmt.Sprintf("nearest %s: %s (%d min walk",
		r.NearestTransitType, r.NearestTransitName, r.NearestTransitWalkMin)
	if len(r.NearestTransitLines) > 0 {
		headline += ", lines " + strings.Join(r.NearestTransitLines, ",")
	}
	headline += ")"
	return headline, nil
}

func renderGeorisques(data any) (string, []string) {
	r, ok := data.(*georisques.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no risk data for these coords", nil
	}
	headline := fmt.Sprintf("%d natural + %d techno risks",
		r.Summary.NaturelsPresentCount, r.Summary.TechnosPresentCount)
	extra := []string{}
	if names := riskKeys(r.Naturels); len(names) > 0 {
		extra = append(extra, "natural: "+strings.Join(names, ", "))
	}
	if names := riskKeys(r.Technos); len(names) > 0 {
		extra = append(extra, "industrial: "+strings.Join(names, ", "))
	}
	if len(r.Summary.RedFlags) > 0 {
		extra = append(extra, "red flags: "+strings.Join(r.Summary.RedFlags, ", "))
	}
	// ReportURL omitted from the default render — it's a 250+-char
	// deeplink with redundant query params. Callers who need it read
	// `gazetteer query --json` and pick `dossier.results.georisques.data.report_url`.
	return headline, extra
}

func renderIRIS(data any) (string, []string) {
	r, ok := data.(*iris.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "outside covered IRIS perimeter", nil
	}
	headline := "IRIS " + r.CodeIRIS
	if r.NomIRIS != "" {
		headline += " — " + r.NomIRIS
	}
	if r.TypIRIS != "" {
		headline += " (" + irisTypeLabel(r.TypIRIS) + ")"
	}
	return headline, nil
}

// irisTypeLabel expands the single-letter INSEE IRIS type code.
func irisTypeLabel(t string) string {
	switch t {
	case "H":
		return "habitat"
	case "A":
		return "activité"
	case "D":
		return "divers"
	case "Z":
		return "commune non découpée"
	default:
		return t
	}
}

func renderCatnat(data any) (string, []string) {
	r, ok := data.(*catnat.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no CatNat decree on record", nil
	}
	headline := fmt.Sprintf("%d arrêtés CatNat (%d récents", r.TotalArretes, r.RecentCount)
	if r.LastEventYear > 0 {
		headline += fmt.Sprintf(", dernier %d", r.LastEventYear)
	}
	if r.Tier != "" {
		headline += ", " + r.Tier
	}
	headline += ")"
	var extra []string
	if len(r.ByCategory) > 0 {
		extra = append(extra, "by category: "+formatMapCounts(r.ByCategory))
	}
	return headline, extra
}

func renderNuisances(data any) (string, []string) {
	r, ok := data.(*nuisances.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "outside the nuisance grid", nil
	}
	headline := fmt.Sprintf("cadre de vie: %s (%d nuisance(s) superposée(s)", r.Tier, r.NuisanceCount)
	if r.PointNoir {
		headline += ", point noir environnemental"
	}
	headline += ")"
	return headline, nil
}

func renderCDSR(data any) (string, []string) {
	r, ok := data.(*cdsr.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no distressed copro within range", nil
	}
	headline := fmt.Sprintf("%d copro(s) en difficulté ≤3 km (plus proche %d m, %d ≤500 m)",
		r.Within3km, r.NearestM, r.Within500m)
	var extra []string
	for _, it := range r.Nearest {
		label := it.Name
		if label == "" {
			label = it.Address
		}
		extra = append(extra, fmt.Sprintf("%d m — %s (%s)", it.DistanceM, label, it.Commune))
	}
	return headline, extra
}

func renderOLL(data any) (string, []string) {
	r, ok := data.(*oll.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no observed-rent cell (Paris intra-muros is out of OLL scope)", nil
	}
	headline := fmt.Sprintf("loyer observé %.1f €/m²/mois", r.ObservedMedianEURPerM2)
	parts := []string{}
	if r.ObservedQ1EURPerM2 > 0 && r.ObservedQ3EURPerM2 > 0 {
		parts = append(parts, fmt.Sprintf("IQR %.1f–%.1f", r.ObservedQ1EURPerM2, r.ObservedQ3EURPerM2))
	}
	if r.SampleSize > 0 {
		parts = append(parts, fmt.Sprintf("%d obs", r.SampleSize))
	}
	if r.Zone != "" {
		zone := r.Zone
		if r.Agglo != "" {
			zone += " · " + r.Agglo
		}
		parts = append(parts, zone)
	}
	if r.Confidence != "" {
		parts = append(parts, "conf="+r.Confidence)
	}
	if len(parts) > 0 {
		headline += " (" + strings.Join(parts, ", ") + ")"
	}
	return headline, nil
}

func renderRPLS(data any) (string, []string) {
	r, ok := data.(*rpls.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from the SRU inventory", nil
	}
	headline := fmt.Sprintf("%.1f%% logements sociaux (SRU)", r.LLSRate)
	if r.Tier != "" {
		headline += " — " + string(r.Tier)
	}
	return headline, nil
}

func renderVacance(data any) (string, []string) {
	r, ok := data.(*vacance.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "absent from the census vacancy dataset", nil
	}
	headline := fmt.Sprintf("vacance INSEE %.1f%%", r.VacancyRate)
	if r.VacantCount > 0 && r.TotalLogements > 0 {
		headline += fmt.Sprintf(" (%d/%d logements", r.VacantCount, r.TotalLogements)
		if r.Tier != "" {
			headline += ", " + string(r.Tier)
		}
		headline += ")"
	} else if r.Tier != "" {
		headline += " (" + string(r.Tier) + ")"
	}
	return headline, nil
}

func renderIPSEcoles(data any) (string, []string) {
	r, ok := data.(*ipsecoles.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no école primaire IPS on record", nil
	}
	headline := fmt.Sprintf("IPS médian %.0f", r.IPSMedian)
	if r.SchoolCount > 0 {
		headline += fmt.Sprintf(" sur %d école(s)", r.SchoolCount)
	}
	if r.Tier != "" {
		headline += " — " + string(r.Tier)
	}
	if r.IPSMin > 0 && r.IPSMax > 0 && r.IPSMin != r.IPSMax {
		headline += fmt.Sprintf(" (band %.0f–%.0f)", r.IPSMin, r.IPSMax)
	}
	return headline, nil
}

func renderCadastre(data any) (string, []string) {
	r, ok := data.(*cadastre.Result)
	if !ok || r == nil || r.IsEmpty() || len(r.Parcels) == 0 {
		return "no parcel at these coordinates", nil
	}
	p := r.Parcels[0]
	headline := fmt.Sprintf("parcelle %s (%d m²)", p.ID, p.ContenanceM2)
	if r.EmpriseRatio != nil {
		headline += fmt.Sprintf(", emprise bâtie %.0f%%", *r.EmpriseRatio*100)
	}
	if len(r.Parcels) > 1 {
		headline += fmt.Sprintf(" +%d autre(s)", len(r.Parcels)-1)
	}
	return headline, nil
}

// formatMapCounts renders a small map[string]int as "k1=v1, k2=v2, …"
// in stable key order.
func formatMapCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(out, ", ")
}

// riskKeys lists the present (Present==true) entries of a Georisques
// risk map in stable order.
func riskKeys(m map[string]georisques.RiskBlob) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v.Present {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
