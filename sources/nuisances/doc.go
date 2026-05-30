// Package nuisances is a gazetteer.Source for the Île-de-France cumulative
// environmental-nuisance grid (Institut Paris Région, combining Bruitparif noise
// and Airparif air-quality layers, 500 m cells).
//
// Each cell carries how many environmental nuisances overlap it (road, rail and
// air traffic noise plus air pollution) — 0 (calm) to 4 (saturated) — and a
// "point noir environnemental" flag for the worst spots. Noise and air exposure
// are documented drivers of property decotes, so this is a cadre-de-vie signal
// a buyer wants before committing.
//
// Given a Listing's coordinates the Source resolves the containing 500 m cell
// (the nearest cell centre within MaxCellMeters) and returns its nuisance count,
// the point-noir flag and a tier. Coverage is Île-de-France only; a point
// outside the grid yields StatusOKEmpty.
//
// The Source is fully offline: a gzipped grid snapshot ships under `data/`,
// refreshable from the region's Opendatasoft portal.
package nuisances
