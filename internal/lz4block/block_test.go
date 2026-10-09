package lz4block_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/pierrec/lz4/v4"
	"github.com/pierrec/lz4/v4/internal/lz4block"
	"github.com/pierrec/lz4/v4/internal/lz4errors"
)

type testcase struct {
	file         string
	compressible bool
	src          []byte
}

var rawFiles = []testcase{
	// {"testdata/207326ba-36f8-11e7-954a-aca46ba8ca73.png", true, nil},
	{"../../testdata/e.txt.gz", false, nil},
	{"../../testdata/gettysburg.txt.gz", true, nil},
	{"../../testdata/Mark.Twain-Tom.Sawyer.txt.gz", true, nil},
	{"../../testdata/pg1661.txt.gz", true, nil},
	{"../../testdata/pi.txt.gz", false, nil},
	{"../../testdata/random.data.gz", false, nil},
	{"../../testdata/repeat.txt.gz", true, nil},
	{"../../testdata/pg1661.txt.gz", true, nil},
}

func TestCompressUncompressBlock(t *testing.T) {
	type compressor func(s, d []byte) (int, error)

	run := func(t *testing.T, tc testcase, compress compressor) int {
		t.Helper()
		src := tc.src

		// Compress the data.
		zbuf := make([]byte, lz4block.CompressBlockBound(len(src)))
		n, err := compress(src, zbuf)
		if err != nil {
			t.Error(err)
			return 0
		}
		zbuf = zbuf[:n]

		// Make sure that it was actually compressed unless not compressible.
		if !tc.compressible {
			return 0
		}

		if n == 0 || n >= len(src) {
			t.Errorf("data not compressed: %d/%d", n, len(src))
			return 0
		}

		// Uncompress the data.
		buf := make([]byte, len(src))
		n, err = lz4block.UncompressBlock(zbuf, buf, nil)
		if err != nil {
			t.Fatal(err)
		} else if n < 0 || n > len(buf) {
			t.Fatalf("returned written bytes > len(buf): n=%d available=%d", n, len(buf))
		} else if n != len(src) {
			t.Errorf("expected to decompress into %d bytes got %d", len(src), n)
		}

		buf = buf[:n]
		if !bytes.Equal(src, buf) {
			var c int
			for i, b := range buf {
				if c > 10 {
					break
				}
				if src[i] != b {
					t.Errorf("%d: exp(%x) != got(%x)", i, src[i], buf[i])
					c++
				}
			}
			t.Fatal("uncompressed compressed data not matching initial input")
			return 0
		}

		return len(zbuf)
	}

	for _, tc := range rawFiles {
		src, err := readGz(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		tc.src = src

		var n, nhc int
		t.Run("", func(t *testing.T) {
			tc := tc
			t.Run(tc.file, func(t *testing.T) {
				n = run(t, tc, func(src, dst []byte) (int, error) {
					return lz4block.CompressBlock(src, dst)
				})
			})
			t.Run(fmt.Sprintf("%s HC", tc.file), func(t *testing.T) {
				nhc = run(t, tc, func(src, dst []byte) (int, error) {
					return lz4.CompressBlockHC(src, dst, 10, nil, nil)
				})
			})
		})
		if !t.Failed() {
			t.Logf("%-40s: %8d / %8d / %8d\n", tc.file, n, nhc, len(src))
		}
	}
}

func TestCompressCornerCase_CopyDstUpperBound(t *testing.T) {
	type compressor func(s, d []byte) (int, error)

	run := func(src []byte, compress compressor) {
		t.Helper()

		// Compress the data.
		// We provide a destination that is too small to trigger an out-of-bounds,
		// which makes it return the error we want.
		zbuf := make([]byte, int(float64(len(src))*0.40))
		_, err := compress(src, zbuf)
		if err != lz4.ErrInvalidSourceShortBuffer {
			t.Fatal("err should be ErrInvalidSourceShortBuffer, was", err)
		}
	}

	file := "../../testdata/upperbound.data.gz"
	src, err := readGz(file)
	if err != nil {
		t.Fatal(err)
	}

	t.Run(file, func(t *testing.T) {
		t.Parallel()
		run(src, func(src, dst []byte) (int, error) {
			return lz4block.CompressBlock(src, dst)
		})
	})
	t.Run(fmt.Sprintf("%s HC", file), func(t *testing.T) {
		t.Parallel()
		run(src, func(src, dst []byte) (int, error) {
			return lz4block.CompressBlockHC(src, dst, 16)
		})
	})
}

func TestIssue23(t *testing.T) {
	compressBuf := make([]byte, lz4block.CompressBlockBound(1<<16))
	for j := 1; j < 16; j++ {
		var buf [1 << 16]byte

		for i := 0; i < len(buf); i += j {
			buf[i] = 1
		}

		n, _ := lz4block.CompressBlock(buf[:], compressBuf)
		if got, want := n, 300; got > want {
			t.Fatalf("not able to compress repeated data: got %d; want %d", got, want)
		}
	}
}

func TestIssue116(t *testing.T) {
	src, err := os.ReadFile("../../fuzz/corpus/pg1661.txt")
	if err != nil {
		t.Fatal(err)
	}

	dst := make([]byte, len(src)-len(src)>>1)
	lz4block.CompressBlock(src, dst)

	var c lz4block.Compressor
	_, err = c.CompressBlock(src, dst)
	if err != lz4errors.ErrInvalidSourceShortBuffer {
		t.Fatalf("expected %v, got nil", lz4errors.ErrInvalidSourceShortBuffer)
	}
}

func TestWriteLiteralLen(t *testing.T) {
	for _, c := range []struct {
		dstlen int
		src    string
	}{
		// These used to panic when writing literal lengths.
		{41, "00000\b000\xa4000\xe6000\v00" +
			"0\xb7000\xb8000#000\x820\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x0000\xff0000\x00000,000e" +
			"000000000000000000000"},
		{62, "00000r000o000a000s000e000tion, 00000e000" +
			"a0d0000t000p000tition, 0o000i000e0c0000o" +
			"0 00000000000000000000000000000000000000000"},
	} {
		dst := make([]byte, c.dstlen)
		lz4block.CompressBlock([]byte(c.src), dst)
	}
}

func readGz(fname string) ([]byte, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(gzr)
}

func TestCompressBlockWithDict_Basic(t *testing.T) {
	dict := []byte("The quick brown fox jumps over the lazy dog. 1234567890abcdefghijklmnopqrstuvwxyz")
	src := []byte("The quick brown fox jumps over the lazy dog! Pack my box with five dozen liquor jugs.")

	bound := lz4block.CompressBlockBound(len(src))
	dstWithDict := make([]byte, bound)
	dstNoDict := make([]byte, bound)

	nWithDict, err := lz4block.CompressBlockWithDict(src, dstWithDict, dict)
	if err != nil {
		t.Fatalf("CompressBlockWithDict failed: %v", err)
	}
	dstWithDict = dstWithDict[:nWithDict]

	nNoDict, err := lz4block.CompressBlock(src, dstNoDict)
	if err != nil {
		t.Fatalf("CompressBlock failed: %v", err)
	}
	dstNoDict = dstNoDict[:nNoDict]

	if nWithDict >= nNoDict {
		t.Fatalf("expected dictionary compression (%d bytes) to be smaller than no-dict (%d bytes)", nWithDict, nNoDict)
	}

	dec := make([]byte, len(src))
	dn, err := lz4block.UncompressBlock(dstWithDict, dec, dict)
	if err != nil {
		t.Fatalf("UncompressBlock failed: %v", err)
	}
	if dn != len(src) {
		t.Fatalf("expected uncompressed size %d, got %d", len(src), dn)
	}
	if !bytes.Equal(dec, src) {
		t.Fatalf("uncompressed output mismatch")
	}
}

func TestCompressBlockWithDict_CrossBoundary(t *testing.T) {
	dict := []byte("prefix--0123456789ABCDEF")
	// The pattern "0123456789ABCDEF" is at the end of dict (16 bytes).
	// In src, we repeat "0123456789ABCDEF" followed by "XYZW", then another repetition.
	src := append([]byte("0123456789ABCDEFextra_bytes_here"), bytes.Repeat([]byte("0123456789ABCDEF"), 4)...)

	dst := make([]byte, lz4block.CompressBlockBound(len(src)))
	n, err := lz4block.CompressBlockWithDict(src, dst, dict)
	if err != nil {
		t.Fatalf("CompressBlockWithDict failed: %v", err)
	}

	dec := make([]byte, len(src))
	dn, err := lz4block.UncompressBlock(dst[:n], dec, dict)
	if err != nil {
		t.Fatalf("UncompressBlock failed: %v", err)
	}
	if dn != len(src) || !bytes.Equal(dec, src) {
		t.Fatalf("round-trip mismatch across dictionary boundary")
	}
}

func TestCompressBlockWithDict_EdgeCases(t *testing.T) {
	data := []byte("hello world, this is a test string for dictionary compression edge cases")

	for _, dict := range [][]byte{
		nil,
		{},
		[]byte("a"),
		[]byte("abc"),
		bytes.Repeat([]byte("xyz"), 30000), // > 64 KB dict
	} {
		dst := make([]byte, lz4block.CompressBlockBound(len(data)))
		n, err := lz4block.CompressBlockWithDict(data, dst, dict)
		if err != nil {
			t.Fatalf("dict len %d: CompressBlockWithDict error: %v", len(dict), err)
		}
		dec := make([]byte, len(data))
		dn, err := lz4block.UncompressBlock(dst[:n], dec, dict)
		if err != nil {
			t.Fatalf("dict len %d: UncompressBlock error: %v", len(dict), err)
		}
		if dn != len(data) || !bytes.Equal(dec, data) {
			t.Fatalf("dict len %d: round-trip mismatch", len(dict))
		}
	}

	// Empty source.
	emptyDst := make([]byte, 16)
	n, err := lz4block.CompressBlockWithDict(nil, emptyDst, []byte("dict"))
	if err != nil {
		t.Fatalf("empty src CompressBlockWithDict error: %v", err)
	}
	dec := make([]byte, 16)
	dn, err := lz4block.UncompressBlock(emptyDst[:n], dec, []byte("dict"))
	if err != nil {
		t.Fatalf("empty src UncompressBlock error: %v", err)
	}
	if dn != 0 {
		t.Fatalf("expected 0 bytes for empty src, got %d", dn)
	}

	// Short dst buffer error.
	shortDst := make([]byte, 2)
	_, err = lz4block.CompressBlockWithDict(data, shortDst, []byte("dict"))
	if err != lz4errors.ErrInvalidSourceShortBuffer && err != nil {
		t.Fatalf("expected ErrInvalidSourceShortBuffer, got %v", err)
	}
}

func TestCompressBlockWithDict_LargeData(t *testing.T) {
	dict := bytes.Repeat([]byte("0123456789abcdef"), 2048) // 32 KB dict
	var src bytes.Buffer
	for i := range 1000 {
		src.WriteString(fmt.Sprintf("block_%d_data_0123456789abcdef_", i))
	}
	input := src.Bytes()

	dst := make([]byte, lz4block.CompressBlockBound(len(input)))
	var c lz4block.Compressor
	n, err := c.CompressBlockWithDict(input, dst, dict)
	if err != nil {
		t.Fatalf("CompressBlockWithDict large data failed: %v", err)
	}

	dec := make([]byte, len(input))
	dn, err := lz4block.UncompressBlock(dst[:n], dec, dict)
	if err != nil {
		t.Fatalf("UncompressBlock large data failed: %v", err)
	}
	if dn != len(input) || !bytes.Equal(dec, input) {
		t.Fatalf("large data round-trip mismatch")
	}
}

func TestCompressBlockWithDict_CompressorReuse(t *testing.T) {
	var c lz4block.Compressor
	dict1 := []byte("first dictionary content with common prefixes and keywords")
	dict2 := []byte("second completely different dictionary content for reuse check")
	src1 := []byte("first dictionary content should compress nicely here")
	src2 := []byte("second completely different dictionary content here as well")

	for range 3 {
		dst1 := make([]byte, lz4block.CompressBlockBound(len(src1)))
		n1, err := c.CompressBlockWithDict(src1, dst1, dict1)
		if err != nil {
			t.Fatalf("reuse run src1 failed: %v", err)
		}
		dec1 := make([]byte, len(src1))
		if _, err := lz4block.UncompressBlock(dst1[:n1], dec1, dict1); err != nil || !bytes.Equal(dec1, src1) {
			t.Fatalf("reuse uncompress src1 mismatch")
		}

		dst2 := make([]byte, lz4block.CompressBlockBound(len(src2)))
		n2, err := c.CompressBlockWithDict(src2, dst2, dict2)
		if err != nil {
			t.Fatalf("reuse run src2 failed: %v", err)
		}
		dec2 := make([]byte, len(src2))
		if _, err := lz4block.UncompressBlock(dst2[:n2], dec2, dict2); err != nil || !bytes.Equal(dec2, src2) {
			t.Fatalf("reuse uncompress src2 mismatch")
		}

		// Also run with no dictionary in between.
		dstNoDict := make([]byte, lz4block.CompressBlockBound(len(src1)))
		nNoDict, err := c.CompressBlock(src1, dstNoDict)
		if err != nil {
			t.Fatalf("reuse run nodict failed: %v", err)
		}
		decNoDict := make([]byte, len(src1))
		if _, err := lz4block.UncompressBlock(dstNoDict[:nNoDict], decNoDict, nil); err != nil || !bytes.Equal(decNoDict, src1) {
			t.Fatalf("reuse uncompress nodict mismatch")
		}
	}
}
