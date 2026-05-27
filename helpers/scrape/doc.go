// Package scrape composes helpers/httpx with goquery to give a 5-line
// "fetch + parse + decode" recipe for HTML scrapers.
//
//   - scrape.ParseHTML(body) wraps goquery.NewDocumentFromReader with a
//     uniform error message. Use this when you already have raw bytes.
//   - scrape.AbsoluteURL(base, ref) resolves a relative href against a
//     base, accepting absolute / root-relative / "./..." / bare-path
//     shapes — the union of every adapter's hand-rolled resolver.
//
// Sub-packages: helpers/scrape/antibot for shared interstitial detection.
package scrape
