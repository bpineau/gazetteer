package zonescore

import (
	"fmt"

	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/oll"
)

// ExampleCompute scores a Dossier. In real use the Dossier comes from
// client.Collect; here it is built directly. A price (DVF) plus a rent (OLL)
// give a gross yield, so the dominant rendement axis is present.
func ExampleCompute() {
	d := dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(300000)), SampleSize: 20}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: 18, SampleSize: 100, Confidence: "high"}),
	)

	score := Compute(d) // or Compute(d, Options{Weights: Personas[ProfileTransport]})

	for _, a := range score.Axes {
		if a.Name == AxisRendement {
			fmt.Printf("rendement axis present: %v\n", a.Present)
		}
	}
	fmt.Printf("composite in [0,100]: %v\n", score.Composite >= 0 && score.Composite <= 100)
	// Output:
	// rendement axis present: true
	// composite in [0,100]: true
}
