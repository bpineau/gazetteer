package gazetteer

import "github.com/bpineau/gazetteer/dataset"

// DatasetProvider is an optional interface a Source MAY implement to expose
// the data files it ships through the dataset package. A block-dataset
// Source (one backed by an embedded or downloaded CSV/JSON artifact rather
// than a live API) returns one dataset.Set per logical dataset it owns.
//
// The CLI's `refresh` command and any library caller that wants to download
// or regenerate datasets discover the work to do by type-asserting each
// configured Source to DatasetProvider and collecting its Sets:
//
//	var sets []dataset.Set
//	for _, src := range sources {
//	    if dp, ok := src.(DatasetProvider); ok {
//	        sets = append(sets, dp.Datasets()...)
//	    }
//	}
//	report, err := dataset.Refresh(ctx, httpClient, sets, dataset.RefreshOptions{})
//
// Live-API Sources (dvf, cadastre, …) do not implement it. The dependency
// edge is one-way: the core may name dataset types, but the dataset package
// never imports the core.
type DatasetProvider interface {
	Datasets() []dataset.Set
}
