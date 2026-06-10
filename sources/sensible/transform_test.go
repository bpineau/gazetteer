package sensible

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/dataset"
)

// memRawSet serves the fixture zip as the raw input.
type memRawSet map[string][]byte

func (m memRawSet) Open(name string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m[name])), nil
}

// buildSHP encodes one type-5 polygon record (a single square ring) into a
// minimal valid shapefile.
func buildSHP(t *testing.T) []byte {
	t.Helper()
	ring := [][2]float64{{2.0, 48.0}, {2.1, 48.0}, {2.1, 48.1}, {2.0, 48.1}, {2.0, 48.0}}

	var rec bytes.Buffer
	le := func(v any) { _ = binary.Write(&rec, binary.LittleEndian, v) }
	le(int32(5))  // shape type: polygon
	for range 4 { // bbox (unused by the parser)
		le(float64(0))
	}
	le(int32(1))         // numParts
	le(int32(len(ring))) // numPoints
	le(int32(0))         // part 0 start
	for _, p := range ring {
		le(p[0])
		le(p[1])
	}

	var shp bytes.Buffer
	be := func(v any) { _ = binary.Write(&shp, binary.BigEndian, v) }
	totalLen := 100 + 8 + rec.Len()
	be(int32(9994))                                          // magic
	shp.Write(make([]byte, 20))                              // unused
	be(int32(totalLen / 2))                                  // file length, 16-bit words
	_ = binary.Write(&shp, binary.LittleEndian, int32(1000)) // version
	_ = binary.Write(&shp, binary.LittleEndian, int32(5))    // shape type
	shp.Write(make([]byte, 64))                              // header bbox
	be(int32(1))                                             // record number
	be(int32(rec.Len() / 2))                                 // content length, 16-bit words
	shp.Write(rec.Bytes())
	return shp.Bytes()
}

// buildDBF encodes a one-record dBASE table with the QRR attribute columns.
func buildDBF(t *testing.T) []byte {
	t.Helper()
	type col struct {
		name  string
		value string
		width int
	}
	cols := []col{
		{"nom", "Zone Fixture", 60},
		{"dep", "93", 10},
		{"service", "DDSP93", 20},
		{"vague", "2.00", 20},
		{"code_qrr", "93_9", 10},
	}
	recordSize := 1
	for _, c := range cols {
		recordSize += c.width
	}
	headerSize := 32 + 32*len(cols) + 1

	var b bytes.Buffer
	b.WriteByte(0x03)
	b.Write([]byte{26, 6, 10})                           // last-update date
	_ = binary.Write(&b, binary.LittleEndian, uint32(1)) // record count
	_ = binary.Write(&b, binary.LittleEndian, uint16(headerSize))
	_ = binary.Write(&b, binary.LittleEndian, uint16(recordSize))
	b.Write(make([]byte, 20)) // reserved
	for _, c := range cols {
		desc := make([]byte, 32)
		copy(desc, c.name)
		desc[11] = 'C'
		desc[16] = byte(c.width)
		b.Write(desc)
	}
	b.WriteByte(0x0D)
	b.WriteByte(' ') // record 0: not deleted
	for _, c := range cols {
		field := make([]byte, c.width)
		for i := range field {
			field[i] = ' '
		}
		copy(field, c.value)
		b.Write(field)
	}
	return b.Bytes()
}

func fixtureZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, blob := range map[string][]byte{
		"contours_test.shp": buildSHP(t),
		"contours_test.dbf": buildDBF(t),
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(blob); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTransformFixture(t *testing.T) {
	raw := memRawSet{rawName: fixtureZip(t)}
	var out bytes.Buffer
	if err := transform(context.Background(), raw, &out); err != nil {
		t.Fatal(err)
	}

	p, err := dataset.ReadGzJSON[processed](bytes.NewReader(out.Bytes()), Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Zones) != 1 {
		t.Fatalf("zones = %d, want 1", len(p.Zones))
	}
	z := p.Zones[0]
	if z.Name != "Zone Fixture" || z.Dep != "93" || z.Vague != 2 || z.Code != "93_9" {
		t.Fatalf("attributes wrong: %+v", z)
	}
	mp := z.G.MultiPolygon()
	if len(mp) != 1 || len(mp[0]) != 1 || len(mp[0][0]) != 5 {
		t.Fatalf("geometry wrong: %+v", mp)
	}
	if got := mp[0][0][1]; math.Abs(got.Lon-2.1) > 1e-9 || math.Abs(got.Lat-48.0) > 1e-9 {
		t.Fatalf("vertex 1 = %+v, want (2.1, 48.0)", got)
	}
	// The point-in-polygon contract on the round-tripped geometry.
	if in, _ := (&Index{feats: []feature{{zone: Zone{Name: z.Name, Kind: KindQRR}, mp: mp, bbox: mp.Bound()}}}).resolve(48.05, 2.05); len(in) != 1 {
		t.Fatalf("fixture square should contain its centre, got %+v", in)
	}
}

// validate must reject an implausibly small artifact (truncated upstream) but
// the fixture-scale failure message should mention the count.
func TestValidateRejectsTinyArtifact(t *testing.T) {
	raw := memRawSet{rawName: fixtureZip(t)}
	var out bytes.Buffer
	if err := transform(context.Background(), raw, &out); err != nil {
		t.Fatal(err)
	}
	err := validate(bytes.NewReader(out.Bytes()))
	if err == nil || !strings.Contains(err.Error(), "implausible zone count") {
		t.Fatalf("validate should reject a 1-zone artifact, got %v", err)
	}
}
