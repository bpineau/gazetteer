package gazetteer_test

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer"
)

// exDvfPayload is a stand-in for what a real Source's typed Result would
// look like. It also satisfies gazetteer.EmptyReporter.
type exDvfPayload struct {
	MedianEurPerM2 int
	SampleSize     int
}

func (p *exDvfPayload) IsEmpty() bool { return p.SampleSize == 0 }

// exSource is a minimal Source implementation that returns a fixed
// exDvfPayload — meant only to make the godoc Example self-contained.
type exSource struct{}

func (*exSource) Name() string { return "ex-dvf" }
func (*exSource) Version() int { return 1 }
func (*exSource) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	return &exDvfPayload{MedianEurPerM2: 9500, SampleSize: 12}, nil
}

func ExampleClient_Collect() {
	client, _ := gazetteer.NewBuilder().With(&exSource{}).Build()
	d := client.Collect(context.Background(), gazetteer.Listing{Address: "10 rue de la Paix"})

	if r, ok := gazetteer.Get[*exDvfPayload](d, "ex-dvf"); ok {
		fmt.Printf("median=%d sample=%d\n", r.MedianEurPerM2, r.SampleSize)
	}
	// Output:
	// median=9500 sample=12
}

func ExampleGet() {
	d := gazetteer.Dossier{
		Results: map[string]gazetteer.Result{
			"ex-dvf": {
				Name:   "ex-dvf",
				Status: gazetteer.StatusOK,
				Data:   &exDvfPayload{MedianEurPerM2: 9500, SampleSize: 12},
			},
		},
	}
	if r, ok := gazetteer.Get[*exDvfPayload](d, "ex-dvf"); ok {
		fmt.Println("median:", r.MedianEurPerM2)
	}
	// Output:
	// median: 9500
}
