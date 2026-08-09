package proto

import (
	"testing"
)

func FuzzParseRange(f *testing.F) {
	for _, seed := range []string{"bytes=0-1", "bytes=", "bytes=abc", "bytes=-5", "range=1-2", "bytes=1-"} {
		f.Add(seed, int64(10))
	}
	f.Fuzz(func(t *testing.T, header string, size int64) {
		if size < 0 {
			size = -size
		}
		_, _, _ = ParseRange(header, size)
	})
}
