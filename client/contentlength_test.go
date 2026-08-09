package client

import (
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
	if hdr.Get("Content-Length") != "5" {
		t.Fatalf("Len 分支 Content-Length 不符：%q", hdr.Get("Content-Length"))
	}

	hdr = http.Header{}
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, []byte("abc"), 0o644)
	f, _ := os.Open(file)
	setContentLength(hdr, f)
	_ = f.Close()
	if hdr.Get("Content-Length") != "3" {
		t.Fatalf("Seeker 分支 Content-Length 不符：%q", hdr.Get("Content-Length"))
	}

	hdr = http.Header{}
	setContentLength(hdr, &seekerErr{})
	if hdr.Get("Content-Length") != "" {
		t.Fatalf("Seek 失败应跳过：%q", hdr.Get("Content-Length"))
	}
	hdr = http.Header{}
	setContentLength(hdr, &seekerErr{failFirst: true})
	if hdr.Get("Content-Length") != "" {
		t.Fatalf("首次 Seek 失败应跳过：%q", hdr.Get("Content-Length"))
	}

	hdr = http.Header{}
	setContentLength(hdr, plainReader{})
	if hdr.Get("Content-Length") != "" {
		t.Fatalf("不可探测应跳过：%q", hdr.Get("Content-Length"))
	}
}
