package client

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/server"
)

func ExampleClient_Put() {
	dir, _ := os.MkdirTemp("", "filex-client-*")
	defer os.RemoveAll(dir)
	store, err := filex.New(filex.Config{DataDir: dir})
	if err != nil {
		panic(err)
	}
	defer store.Close()
	ts := httptest.NewServer(server.NewHandler(server.HandlerConfig{Store: store}))
	defer ts.Close()

	c, err := New(ts.URL)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "demo")
	_, err = c.Put(ctx, "demo", "hello.txt", strings.NewReader("你好，filex"),
		filex.PutOptions{ContentType: "text/plain"})
	if err != nil {
		panic(err)
	}
	obj, err := c.Get(ctx, "demo", "hello.txt", filex.GetOptions{Verify: true})
	if err != nil {
		panic(err)
	}
	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	fmt.Printf("读取：%s\n", data)
	// Output: 读取：你好，filex
}
