package proto

import (
	"testing"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/testx"
)

func TestParseRange(t *testing.T) {
	_, ok, err := ParseRange("", 10)
	testx.RequireFalse(t, ok)
	testx.RequireNoError(t, err)

	cases := []struct {
		header string
		size   int64
		want   filex.ByteRange
	}{
		{"bytes=1-3", 10, filex.ByteRange{Start: 1, End: 3}},
		{"bytes=3-", 10, filex.ByteRange{Start: 3, End: 9}},
		{"bytes=3-100", 10, filex.ByteRange{Start: 3, End: 9}},
		{"bytes=0-0", 10, filex.ByteRange{Start: 0, End: 0}},
		{"bytes=-5", 10, filex.ByteRange{Start: 5, End: 9}},
		{"bytes=-100", 10, filex.ByteRange{Start: 0, End: 9}},
	}
	for _, c := range cases {
		got, ok, err := ParseRange(c.header, c.size)
		testx.RequireNoError(t, err)
		testx.RequireTrue(t, ok)
		testx.RequireEqual(t, got, c.want)
	}

	invalid := []struct {
		header string
		size   int64
	}{
		{"range=1-2", 10},
		{"bytes=", 10},
		{"bytes=1", 10},
		{"bytes=abc-2", 10},
		{"bytes=-1-2", 10},
		{"bytes=1-", 0},
		{"bytes=1-2", 0},
		{"bytes=5-9", 5},
		{"bytes=-0", 10},
		{"bytes=3-1", 10},
		{"bytes=1-abc", 10},
		{"bytes=-", 10},
	}
	for _, c := range invalid {
		_, ok, err := ParseRange(c.header, c.size)
		testx.RequireTrue(t, ok)
		testx.RequireErrCode(t, err, filex.CodeInvalidRange)
	}
}
