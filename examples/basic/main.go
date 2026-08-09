// 基础用法示例：创建桶、写入对象、读取对象、枚举对象、删除对象。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lcylpzls/filex"
)

func run(dataDir string) error {
	ctx := context.Background()
	store, err := filex.New(filex.Config{DataDir: dataDir})
	if err != nil {
		return err
	}
	defer store.Close()

	if _, err := store.CreateBucket(ctx, "demo-bucket"); err != nil {
		return err
	}

	content := "你好，filex 对象存储"
	info, err := store.Put(ctx, "demo-bucket", "notes/hello.txt",
		strings.NewReader(content), filex.PutOptions{
			ContentType: "text/plain; charset=utf-8",
			Metadata:    map[string]string{"示例": "基础用法"},
		})
	if err != nil {
		return err
	}
	fmt.Printf("写入对象：%s/%s，大小 %d 字节，ETag=%s\n",
		info.Bucket, info.Key, info.Size, info.ETag)

	obj, err := store.Get(ctx, "demo-bucket", "notes/hello.txt", filex.GetOptions{Verify: true})
	if err != nil {
		return err
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		_ = obj.Close()
		return err
	}
	_ = obj.Close()
	fmt.Printf("读取内容：%s\n", data)

	result, err := store.List(ctx, "demo-bucket", filex.ListOptions{Prefix: "notes/"})
	if err != nil {
		return err
	}
	for _, o := range result.Objects {
		fmt.Printf("枚举对象：%s（%d 字节）\n", o.Key, o.Size)
	}

	if err := store.Delete(ctx, "demo-bucket", "notes/hello.txt"); err != nil {
		return err
	}
	fmt.Println("删除对象成功")
	return nil
}

func main() {
	dir := "data"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
