package client

import (
	"github.com/lcylpzls/testx"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type plainReader struct{}

func (plainReader) Read([]byte) (int, error) { return 0, io.EOF }

type seekerErr struct {
	step      int
	failFirst bool
}

func (s *seekerErr) Read([]byte) (int, error) { return 0, io.EOF }
func (s *seekerErr) Seek(offset int64, whence int) (int64, error) {
	if s.failFirst {
		return 0, errSeek
	}
	if s.step == 0 {
		s.step++
		return 0, nil
	}
	return 0, errSeek
}

var errSeek = io.ErrUnexpectedEOF

func TestSetContentLength(t *testing.T) {
	hdr := http.Header{}
	setContentLength(hdr, strings.NewReader("hello"))
	testx.RequireEqual(t, hdr.Get("Content-Length"), "5")

	hdr = http.Header{}
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, []byte("abc"), 0o644)
	f, _ := os.Open(file)
	setContentLength(hdr, f)
	_ = f.Close()
	testx.RequireEqual(t, hdr.Get("Content-Length"), "3")

	hdr = http.Header{}
	setContentLength(hdr, &seekerErr{})
	testx.RequireEqual(t, hdr.Get("Content-Length"), "")
	hdr = http.Header{}
	setContentLength(hdr, &seekerErr{failFirst: true})
	testx.RequireEqual(t, hdr.Get("Content-Length"), "")

	hdr = http.Header{}
	setContentLength(hdr, plainReader{})
	testx.RequireEqual(t, hdr.Get("Content-Length"), "")
}
