package proto

import (
	"testing"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
)

func TestParseRange(t *testing.T) {
	if _, ok, err := ParseRange("", 10); ok || err != nil {
		t.Fatalf("空头应返回无范围：ok=%v err=%v", ok, err)
	}

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
		if err != nil || !ok || got != c.want {
			t.Errorf("ParseRange(%q, %d) = %+v, %v, %v；期望 %+v",
				c.header, c.size, got, ok, err, c.want)
		}
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
		if err == nil || !ok {
			t.Errorf("ParseRange(%q, %d) 应报错：ok=%v err=%v", c.header, c.size, ok, err)
			continue
		}
		if !errx.Is(err, filex.CodeInvalidRange) {
			t.Errorf("错误码应为 filex_invalid_range：%v", err)
		}
	}
}
