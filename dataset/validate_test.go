package dataset

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func gzipOf(s string) io.Reader {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return &buf
}

func TestValidateProcessed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		body io.Reader
		ok   bool
	}{
		{"json_clean", "x.json", strings.NewReader(`{"a":1}`), true},
		{"json_trailing_ws", "x.json", strings.NewReader("{\"a\":1}\n  "), true},
		{"json_trailing_value", "x.json", strings.NewReader(`{"a":1}{"b":2}`), false},
		{"json_garbage", "x.json", strings.NewReader(`{"a":1} oops`), false},
		{"json_empty", "x.json", strings.NewReader(``), false},
		{"csv_ok", "x.csv", strings.NewReader("a;b;c\n1;2;3\n"), true},
		{"csv_empty", "x.csv", strings.NewReader(``), false},
		{"gz_json_ok", "x.json.gz", gzipOf(`{"a":1}`), true},
		{"plain_nonempty", "x.dat", strings.NewReader("anything"), true},
		{"plain_empty", "x.dat", strings.NewReader(``), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProcessed(tc.file, tc.body)
			if tc.ok && err != nil {
				t.Errorf("want pass, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Errorf("want fail, got nil")
			}
		})
	}
}
