package osm

import (
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/osm_transit_stations.json.gz
var embedFS embed.FS

// set binds the embedded baseline station catalog to the datadir/refresh
// pipeline. Unlike the file-download Sources, osm's refresh Transform has no
// static raw input: it rebuilds the catalog from a live Overpass refresh
// (one query per department), using the HTTP client dataset.Refresh injects
// into the context.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "osm_transit_stations.json.gz"},
	Transform: transform,
	Validate:  validate,
}

var (
	catalogOnce  sync.Once
	catalogCache *Catalog
	catalogErr   error
)

// Load returns the station catalog, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded baseline and parsing it
// once. The dir from the first call wins for the process lifetime. A catalog
// present neither in the datadir nor embedded yields an empty catalog — the
// Source then relies on its live Overpass fallback (when configured).
func Load(dir string) (*Catalog, error) {
	catalogOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			catalogCache = &Catalog{}
			return
		}
		if err != nil {
			catalogErr = fmt.Errorf("osm: open catalog: %w", err)
			return
		}
		defer func() { _ = rc.Close() }()
		cat, err := parseCatalog(rc)
		if err != nil {
			catalogErr = err
			return
		}
		catalogCache = cat
	})
	return catalogCache, catalogErr
}

// parseCatalog decodes the gzipped JSON catalog.
func parseCatalog(r io.Reader) (*Catalog, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("osm: gunzip catalog: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("osm: read catalog: %w", err)
	}
	var cat Catalog
	if err := json.Unmarshal(body, &cat); err != nil {
		return nil, fmt.Errorf("osm: parse catalog: %w", err)
	}
	return &cat, nil
}

// transform rebuilds the catalog from a live Overpass refresh (one query per
// metropolitan department) and writes it gzip-compressed. It uses the HTTP
// client dataset.Refresh injects into ctx.
func transform(ctx context.Context, _ dataset.RawSet, dst io.Writer) error {
	c := dataset.HTTPClient(ctx)
	if c == nil {
		return errors.New("osm: refresh requires an HTTP client (none in context)")
	}
	cat, err := RefreshCatalogFromOverpassByDepts(ctx, NewHTTPOverpassFetcher(c, ""), nil)
	if err != nil {
		return fmt.Errorf("osm: overpass refresh: %w", err)
	}
	raw, err := json.Marshal(cat)
	if err != nil {
		return fmt.Errorf("osm: marshal catalog: %w", err)
	}
	zw := gzip.NewWriter(dst)
	if _, err := zw.Write(raw); err != nil {
		return err
	}
	return zw.Close()
}

// validate gates publication: the rebuilt catalog must parse and carry
// stations.
func validate(r io.Reader) error {
	cat, err := parseCatalog(r)
	if err != nil {
		return err
	}
	if cat == nil || len(cat.Stations) == 0 {
		return errors.New("osm: validated catalog has no stations")
	}
	return nil
}
