package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/pierrec/lz4/v4"
)

func TestCompressLevels(t *testing.T) {
	input := bytes.Repeat([]byte("hello world this is a test and we want to verify compression levels "), 100)

	compress := func(t *testing.T, level string) ([]byte, error) {
		t.Helper()
		fs := flag.NewFlagSet("compress", flag.ContinueOnError)
		handler := Compress(fs)
		if err := fs.Parse([]string{"-l", level}); err != nil {
			t.Fatalf("fs.Parse failed: %v", err)
		}

		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "input.txt")
		if err := os.WriteFile(srcFile, input, 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		if _, err := handler(srcFile); err != nil {
			return nil, err
		}
		return os.ReadFile(srcFile + lz4Extension)
	}

	tests := []struct {
		name     string
		levels   []string
		wantDiff bool
		wantErr  error
	}{
		{
			name:     "levels 0 and 1 differ",
			levels:   []string{"0", "1"},
			wantDiff: true,
		},
		{
			name:    "level 10 invalid",
			levels:  []string{"10"},
			wantErr: lz4.ErrOptionInvalidCompressionLevel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outputs [][]byte
			for _, lvl := range tc.levels {
				out, err := compress(t, lvl)
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("got error %v, want %v", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("compress -l %s failed: %v", lvl, err)
				}
				outputs = append(outputs, out)
			}
			if tc.wantDiff && len(outputs) == 2 && bytes.Equal(outputs[0], outputs[1]) {
				t.Fatalf("-l %s and -l %s produced identical output", tc.levels[0], tc.levels[1])
			}
		})
	}
}
