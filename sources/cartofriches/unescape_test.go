package cartofriches

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// wantUnescaped is the reference semantics the streaming unescaper must
// reproduce exactly: every backslash-escaped quote becomes a doubled quote,
// leftmost-first and non-overlapping; every other byte, lone trailing
// backslash included, is passed through untouched.
func wantUnescaped(in string) string {
	return strings.ReplaceAll(in, `\"`, `""`)
}

// chunkReader hands out at most size bytes per Read, so the unescaper's
// chunk-boundary bookkeeping (the held-back backslash) is exercised at every
// possible split point.
type chunkReader struct {
	data []byte
	size int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(min(len(p), c.size), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func TestUnescapeQuotes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no escape", `"61386";"friche d'habitat";6507`, `"61386";"friche d'habitat";6507`},
		{"one escaped quote", `maison \"Les Opalines\"`, `maison ""Les Opalines""`},
		{"escaped quote at the very start", `\"x`, `""x`},
		{"escaped quote at the very end", `x\"`, `x""`},
		{"lone trailing backslash", `x\`, `x\`},
		{"lone backslash mid-cell", `C:\temp;x`, `C:\temp;x`},
		{"double backslash then quote", `a\\"b`, `a\""b`},
		{"only backslashes", `\\\\`, `\\\\`},
		{"consecutive escapes", `\"\"\"`, `""""""`},
		{"already doubled quotes", `""x""`, `""x""`},
		{"multi-line", "a\\\"b\nc\\\"d\n", "a\"\"b\nc\"\"d\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.want != wantUnescaped(c.in) {
				t.Fatalf("test case is inconsistent with the reference: %q vs %q", c.want, wantUnescaped(c.in))
			}
			// Read through in one gulp, then re-read the same input split
			// into every chunk size that matters: the held-back backslash
			// must survive a split at any byte.
			for _, size := range []int{1, 2, 3, 5, 64, 1 << 16} {
				got, err := io.ReadAll(unescapeQuotes(&chunkReader{data: []byte(c.in), size: size}))
				if err != nil {
					t.Fatalf("chunk=%d: ReadAll: %v", size, err)
				}
				if string(got) != c.want {
					t.Errorf("chunk=%d: got %q, want %q", size, got, c.want)
				}
			}
		})
	}
}

// TestUnescapeQuotes_TinyDestination drains the reader one byte at a time:
// an expansion wider than the caller's buffer has to be buffered and handed
// back on the following calls, in order and without loss.
func TestUnescapeQuotes_TinyDestination(t *testing.T) {
	t.Parallel()
	in := `a\"b\"c\`
	r := unescapeQuotes(strings.NewReader(in))
	var out bytes.Buffer
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		out.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if out.Len() > 4*len(in) {
			t.Fatal("reader does not terminate")
		}
	}
	if got := out.String(); got != wantUnescaped(in) {
		t.Errorf("byte-at-a-time = %q, want %q", got, wantUnescaped(in))
	}
}

// TestUnescapeQuotes_Exhaustive walks every string over the alphabet
// {a, backslash, quote} up to length 5 through every chunk size up to 4,
// against the reference. This is the cheap way to be sure the streaming
// state machine has no split-dependent hole.
func TestUnescapeQuotes_Exhaustive(t *testing.T) {
	t.Parallel()
	alphabet := []byte{'a', '\\', '"'}
	var walk func(prefix []byte, depth int)
	walk = func(prefix []byte, depth int) {
		in := string(prefix)
		want := wantUnescaped(in)
		for size := 1; size <= 4; size++ {
			got, err := io.ReadAll(unescapeQuotes(&chunkReader{data: []byte(in), size: size}))
			if err != nil {
				t.Fatalf("in=%q chunk=%d: ReadAll: %v", in, size, err)
			}
			if string(got) != want {
				t.Fatalf("in=%q chunk=%d: got %q, want %q", in, size, got, want)
			}
		}
		if depth == 0 {
			return
		}
		for _, b := range alphabet {
			walk(append(prefix, b), depth-1)
		}
	}
	walk(nil, 5)
}
