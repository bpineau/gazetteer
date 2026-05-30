// Package oll is a gazetteer.Source for observed market rents from the
// Observatoires Locaux des Loyers (OLL) — the network of approved local rent
// observatories. Unlike encadrement (the legal rent cap) or carteloyers (a
// national model), OLL publishes rents actually observed in the field, so it is
// the most representative "what does this let for?" signal.
//
// Scope: the major French agglomerations covered by the OLL network — the Paris
// petite/grande couronne ("agglomération parisienne hors Paris", L7502), Lyon,
// Lille, Toulouse, Bordeaux, Nantes, Strasbourg, Montpellier, Grenoble, Rennes,
// Nice, Clermont-Ferrand, Nancy, Tours, La Rochelle, Besançon and La Réunion.
// Paris intra-muros is intentionally excluded (its finer OLL zone layout doesn't
// fit the join, and encadrement serves Paris rents). The set is curated in
// aggloSpecs; extend it (and re-run refresh) to cover more perimeters.
//
// Resolution: the listing's INSEE commune maps to an OLL zone (the observatory
// splits its perimeter into a handful of geographic zones), then the Source
// returns the observed median €/m²/month for the matching rooms bucket, with
// the inter-quartile band and the sample size. The typed Result satisfies
// appraisal.RentEstimator, so OLL feeds the consolidated rent synthesis.
//
// The Source is fully offline: an embedded snapshot ships under `data/`,
// refreshable from the observatory's published per-agglo archive.
package oll
