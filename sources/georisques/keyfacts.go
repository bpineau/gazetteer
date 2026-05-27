// keyfacts.go — pure helper transforms from the Georisques result
// payload (as decoded into a map[string]any from EnrichPayload.Result)
// into the few "key facts" callers want to surface front-and-center on
// the auction detail page.
//
// Like sources/bdnb/keyfacts.go, this file is read-only over an
// already-marshalled JSON tree. Adding a key fact does NOT change the
// parser, the fetcher, or the persisted payload — it just projects
// what's already there.
package georisques

import "strings"

// ExtractRedFlags returns the deduplicated, lowercase list of red-flag
// risk slugs Georisques has surfaced on this address (e.g. "inondation",
// "retrait_argile", "seisme", "radon"). Empty slice + false when no
// red-flags array is present in the payload.
//
// The slugs are passed through as-is (no humanisation) because the
// enrichview layer already maps them to a chip set.
func ExtractRedFlags(result map[string]any) ([]string, bool) {
	summary, _ := result["summary"].(map[string]any)
	if summary == nil {
		return nil, false
	}
	raw, ok := summary["red_flags"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// ExtractActiveNaturalRisks walks `result.naturels` (the per-risk
// detail map) and returns the deduplicated, lowercase slugs whose
// `present` field is true. This is a richer signal than the red-flag
// list (which only flags subjectively scary risks); a buyer also wants
// to know about, say, an active "mvt_terrain" (movement of terrain)
// even if it's not on the red-flag short list.
//
// Returns (nil, false) when `naturels` is absent / empty.
func ExtractActiveNaturalRisks(result map[string]any) ([]string, bool) {
	naturels, _ := result["naturels"].(map[string]any)
	if len(naturels) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(naturels))
	for slug, raw := range naturels {
		blob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		present, _ := blob["present"].(bool)
		if !present {
			continue
		}
		s := strings.TrimSpace(strings.ToLower(slug))
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	// Deterministic order so the rendered string is stable across
	// requests (map iteration order in Go is randomized).
	sortStringsInPlace(out)
	return out, true
}

// ExtractActiveTechnoRisks mirrors ExtractActiveNaturalRisks for the
// `result.technos` map. Tech risks (ICPE, sites pollues, transport de
// matieres dangereuses) are typically rare but high-impact.
func ExtractActiveTechnoRisks(result map[string]any) ([]string, bool) {
	technos, _ := result["technos"].(map[string]any)
	if len(technos) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(technos))
	for slug, raw := range technos {
		blob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		present, _ := blob["present"].(bool)
		if !present {
			continue
		}
		s := strings.TrimSpace(strings.ToLower(slug))
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sortStringsInPlace(out)
	return out, true
}

// sortStringsInPlace is a small, alloc-free in-place sort. We avoid
// importing "sort" here purely to keep the helper file's import
// surface minimal — the cost is the same on the typical 0..6 input.
func sortStringsInPlace(s []string) {
	// insertion sort: O(n²) but n ≤ ~12 in practice.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
