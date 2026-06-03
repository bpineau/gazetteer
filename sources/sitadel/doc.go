// Package sitadel is a gazetteer.Source that returns per-commune
// housing-construction dynamics from the SDES Sitadel annual file: building
// permits AUTHORISED (LOG_AUT) and housing STARTS (LOG_COM, "logements
// commencés"), counted in dwellings.
//
// The signal is the forward-looking SUPPLY side of the rental market. A
// commune that authorises and breaks ground on a lot of new housing relative
// to its existing stock is adding competing supply — a headwind on rents and
// resale prices — whereas near-zero construction in an already tense market
// reinforces scarcity in the landlord's favour. The "Collectif" (apartment)
// share isolates the segment most relevant to rental investors.
//
// The Result is deliberately RAW: absolute dwelling counts scale with commune
// size, so a single number is only comparable once normalised by the existing
// stock. That normalisation is an appraisal-layer concern (cf. taxefonciere
// and gpe, which likewise avoid baking a misleading composite tier into the
// source). The Source exposes the latest year, a 5-year mean (more stable
// than one noisy year), the collectif share and the full per-year authorised
// series for a sparkline.
//
// Data quirks reflected by the Source:
//
//   - LOG_AUT (authorised) is always populated. LOG_COM (started) is blank
//     for the freshest, provisional millésime (starts are not yet
//     consolidated), so StartedLatest is typically one year behind
//     LatestYear — hence the separate StartedLatestYear field.
//   - A blank numeric cell means "no data", NOT zero. The transform keeps the
//     distinction so a true 0 (e.g. a small commune that built nothing) is
//     not conflated with a missing measurement.
//
// Paris / Lyon / Marseille keying: the Sitadel file publishes BOTH the
// aggregate parent commune (75056 Paris, 69123 Lyon, 13055 Marseille) AND, for
// Paris and Lyon only, the per-arrondissement codes (75101..75120,
// 69381..69389) — Marseille arrondissements (13201..13216) are absent. To
// return a single, consistent commune-wide supply figure (and to handle
// Marseille at all), the Source FOLDS arrondissement INSEE onto the parent via
// communes.FoldArrondissement before lookup, mirroring sources/rpls. The
// embedded artifact stores only the parent-commune aggregates; the redundant
// Paris/Lyon arrondissement rows are dropped at build time.
//
// Granularity is the commune (5-digit INSEE). Coverage is national —
// metropolitan France plus the DOM.
//
// The Source is fully offline: the merged dataset ships embedded under
// data/sitadel.json.gz.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type and surface are irrelevant — construction dynamics are a
// commune-wide attribute.
package sitadel
