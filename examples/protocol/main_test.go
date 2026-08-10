package main

import (
	"bytes"
	testx "github.com/lcylpzls/testx"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/filex/server"
)

func TestProtocolRoundTrip(t *testing.T) {
	store, err := filex.New(filex.Config{DataDir: t.TempDir()})
	testx.RequireNoError(t, err)

	defer store.Close()
	ts := httptest.NewServer(server.NewHandler(server.HandlerConfig{Store: store}))
	defer ts.Close()

	c, err := client.New(ts.URL)
	testx.RequireNoError(t, err)

	if _, err := c.CreateBucket(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(t.Context(), "demo", "k", strings.NewReader("v"), filex.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	obj, err := c.Get(t.Context(), "demo", "k", filex.GetOptions{})
	testx.RequireNoError(t, err)

	_ = obj.Close()
}

func TestProtocolWithAuthAndEncryption(t *testing.T) {
	dir := t.TempDir()
	testx.RequireNoError(t, runWithOptions("127.0.0.1:0", dir, "tok", bytes.Repeat([]byte{1}, 32)))
}
