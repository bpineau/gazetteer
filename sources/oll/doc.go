// Package oll is a gazetteer.Source for observed market rents from the
// Observatoires Locaux des Loyers (OLL) — the network of approved local rent
// observatories. Unlike encadrement (the legal rent cap) or carteloyers (a
// national model), OLL publishes rents actually observed in the field, so it is
// the most representative "what does this let for?" signal.
//
// Scope (v1): the Paris-region observatory perimeter "agglomération parisienne
// hors Paris" (OLL code L7502) — the petite/grande-couronne communes around
// Paris, which is the priority zone for a banlieue investor. The design is
// extensible: more OLL agglomerations are added by listing their per-agglo
// archive in the transform.
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
