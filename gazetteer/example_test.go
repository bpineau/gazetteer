package gazetteer_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bpineau/gazetteer/gazetteer"
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

// ExampleRegister shows the plugin-source pattern: a custom Source
// declared out-of-tree registers its typed payload via init() so a
// JSON-roundtripped Dossier can reconstitute concrete types.
//
// Once Register has been called, gazetteer.Get[*PluginPayload] works
// against a Dossier reconstituted via json.Unmarshal — without the
// registration, the typed Data would silently be left nil.
func ExampleRegister() {
	// In a real plugin this would live in `func init()`.
	const pluginName = "ex-plugin"
	gazetteer.Register(pluginName, func() any { return &exDvfPayload{} })

	// Simulate the wire payload as if it had come from a remote
	// service that serialised a Dossier earlier.
	wire := []byte(`{
        "listing": {"address":"10 rue de la Paix"},
        "results": {
            "ex-plugin": {
                "name":"ex-plugin",
                "version":1,
                "status":"ok",
                "data":{"MedianEurPerM2":9500,"SampleSize":12}
            }
        }
    }`)

	var d gazetteer.Dossier
	if err := json.Unmarshal(wire, &d); err != nil {
		fmt.Println("unmarshal:", err)
		return
	}
	if r, ok := gazetteer.Get[*exDvfPayload](d, pluginName); ok {
		fmt.Printf("recovered: median=%d sample=%d\n", r.MedianEurPerM2, r.SampleSize)
	}
	// Output:
	// recovered: median=9500 sample=12
}

// ExampleResult_IsEmpty shows how to inspect the IsEmpty convenience on
// a Result without knowing the concrete Data type.
func ExampleResult_IsEmpty() {
	r := gazetteer.Result{
		Name:   "ex-dvf",
		Status: gazetteer.StatusOKEmpty,
		Data:   &exDvfPayload{SampleSize: 0},
	}
	if r.IsEmpty() {
		fmt.Println("source reported no useful data")
	}
	// Output:
	// source reported no useful data
}
