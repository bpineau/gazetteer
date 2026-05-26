package frnorm

// Allocation + throughput benchmarks for the hot normalisers in this
// package. These functions live on every scraper's row-mapping path
// (lawyer / avoventes / vench / licitor) and on the matcher's name
// canonicalisation path. A regression in allocation count would compound
// across millions of rows during a full re-consolidation, so we pin a
// baseline here.
//
// Run :
//
//	go test -bench=. -benchmem ./pkg/frnorm/
//	go test -bench=BenchmarkParseFRPriceToCentimes -benchmem ./pkg/frnorm/
//
// Baseline on Apple M1 Max, Go 1.26 :
//
//	BenchmarkStripAccents_Mixed-10                  ~470 ns/op   144 B/op   1 allocs/op
//	BenchmarkStripAccents_PureASCII-10              ~235 ns/op    80 B/op   1 allocs/op
//	BenchmarkNormaliseSpace_Typical-10              ~190 ns/op    64 B/op   1 allocs/op
//	BenchmarkParseFRPriceToCentimes_Plain-10        ~160 ns/op    16 B/op   2 allocs/op
//	BenchmarkParseFRPriceToCentimes_Thousands-10    ~220 ns/op    48 B/op   3 allocs/op
//	BenchmarkParseFRPriceToCentimes_DotThousands-10 ~165 ns/op    32 B/op   2 allocs/op
//	BenchmarkExtractZipCity_Typical-10             ~1730 ns/op   112 B/op   2 allocs/op
//	BenchmarkExtractZipCity_NoMatch-10             ~1860 ns/op     0 B/op   0 allocs/op
//	BenchmarkExtractZipFromAddress_Typical-10       ~360 ns/op    32 B/op   1 allocs/op
//	BenchmarkNormalizeHearingTime_Typical-10        ~370 ns/op   136 B/op   3 allocs/op
//	BenchmarkNormalizeHearingTime_FullForm-10       ~375 ns/op     0 B/op   0 allocs/op
//
// (Wall-clock varies across hardware; allocs/op is the reliable invariant.)

import "testing"

func BenchmarkStripAccents_Mixed(b *testing.B) {
	const input = "ÀÁÂÃÄÅàáâãäå-ÈÉÊËèéêë-ÒÓÔÕÖòóôõö ÇçÑñ Œœ Ææ-Crédit Mutuel Arkéa, 23 rue de l'Étoile, Saône-et-Loire"
	b.ReportAllocs()
	for b.Loop() {
		_ = StripAccents(input)
	}
}

func BenchmarkStripAccents_PureASCII(b *testing.B) {
	const input = "SCP Cabinet Dupont Martin, 14 avenue de la Republique, 75011 Paris"
	b.ReportAllocs()
	for b.Loop() {
		_ = StripAccents(input)
	}
}

func BenchmarkNormaliseSpace_Typical(b *testing.B) {
	const input = "  3 bis  Av.\tdu\nPresident\r\nde la Republique, 75011 Paris  "
	b.ReportAllocs()
	for b.Loop() {
		_ = NormaliseSpace(input)
	}
}

func BenchmarkParseFRPriceToCentimes_Plain(b *testing.B) {
	const input = "150,50 €"
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseFRPriceToCentimes(input)
	}
}

func BenchmarkParseFRPriceToCentimes_Thousands(b *testing.B) {
	const input = "1 336 500,38 €"
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseFRPriceToCentimes(input)
	}
}

func BenchmarkParseFRPriceToCentimes_DotThousands(b *testing.B) {
	const input = "1.336.500,38"
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseFRPriceToCentimes(input)
	}
}

func BenchmarkExtractZipCity_Typical(b *testing.B) {
	const input = "3 bis Av. du President de la Republique, 93110 Rosny-sous-Bois, France"
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = ExtractZipCity(input)
	}
}

func BenchmarkExtractZipCity_NoMatch(b *testing.B) {
	// Worst case for the regex engine — long input, no zip token.
	const input = "Lorem ipsum dolor sit amet consectetur adipiscing elit, no postal code here at all, definitely none"
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = ExtractZipCity(input)
	}
}

func BenchmarkExtractZipFromAddress_Typical(b *testing.B) {
	const input = "12 rue Foo, 75011 Paris, France"
	b.ReportAllocs()
	for b.Loop() {
		_, _ = ExtractZipFromAddress(input)
	}
}

func BenchmarkNormalizeHearingTime_Typical(b *testing.B) {
	const input = "14h30"
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeHearingTime(input)
	}
}

func BenchmarkNormalizeHearingTime_FullForm(b *testing.B) {
	const input = "14 heures 30 minutes"
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeHearingTime(input)
	}
}
