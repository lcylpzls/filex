package core_test

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lcylpzls/filex"
)

func ExampleStore_Put() {
	dir, _ := os.MkdirTemp("", "filex-example-*")
	defer os.RemoveAll(dir)
	store, err := filex.New(filex.Config{DataDir: dir})
	if err != nil {
		panic(err)
	}
	defer store.Close()

	ctx := context.Background()
	_, _ = store.CreateBucket(ctx, "demo")
	info, err := store.Put(ctx, "demo", "hello.txt",
		strings.NewReader("你好，filex"), filex.PutOptions{ContentType: "text/plain"})
	if err != nil {
		panic(err)
	}
	obj, err := store.Get(ctx, "demo", "hello.txt", filex.GetOptions{Verify: true})
	if err != nil {
		panic(err)
	}
	data := make([]byte, 0, info.Size)
	buf := make([]byte, 32)
	for {
		n, err := obj.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil {
			break
		}
	}
	_ = obj.Close()
	fmt.Printf("内容：%s（%d 字节）\n", data, info.Size)
	// Output: 内容：你好，filex（14 字节）
}
