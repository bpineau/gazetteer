package factory_test

import (
	"context"
	"fmt"
	"log"

	"github.com/bpineau/gazetteer/factory"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/dvf"
)

// ExampleNewDefault shows the canonical end-to-end flow: build a Client
// with every stable Source wired, normalize a free-text address, collect
// the Dossier, then pull one typed Result out of it.
func ExampleNewDefault() {
	ctx := context.Background()

	client, err := factory.NewDefault(ctx)
	if err != nil {
		log.Fatal(err)
	}

	listing, err := client.Normalize(ctx, "1 rue de Rivoli, 75001 Paris")
	if err != nil {
		log.Fatal(err)
	}

	dossier := client.Collect(ctx, listing)

	// Every price in dvf.Result is integer centimes; the unit always
	// lives in the field name.
	if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok && r.ValueEURPerM2Cents != nil {
		fmt.Printf("median sale price: %d €/m²\n", *r.ValueEURPerM2Cents/100)
	}
}

// ExampleNewDefaultWith prunes Sources the caller never consumes,
// cutting their latency and failure surface while keeping the rest of
// the default roster (including Sources added in later releases).
func ExampleNewDefaultWith() {
	ctx := context.Background()

	client, err := factory.NewDefaultWith(ctx, factory.Options{
		Exclude: []string{"bdnb", "locservice"},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(client.SourceNames()) // the pruned roster, sorted
}
