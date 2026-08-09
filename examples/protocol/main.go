// 协议端到端示例：启动自研协议服务端，用客户端完成桶与对象的全流程。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/filex/server"
)

func run(addr, dataDir string) error {
	store, err := filex.New(filex.Config{DataDir: dataDir})
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{
		Handler: server.NewHandler(server.HandlerConfig{Store: store}),
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	fmt.Printf("filex 协议服务已启动：http://%s\n", ln.Addr())

	ctx := context.Background()
	c, err := client.New("http://" + ln.Addr().String())
	if err != nil {
		return err
	}
	_, _ = c.CreateBucket(ctx, "demo-bucket")
	_, err = c.Put(ctx, "demo-bucket", "notes/hello.txt",
		strings.NewReader("你好，filex 协议"), filex.PutOptions{ContentType: "text/plain"})
	if err != nil {
		return err
	}
	obj, err := c.Get(ctx, "demo-bucket", "notes/hello.txt", filex.GetOptions{Verify: true})
	if err != nil {
		return err
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil {
		return err
	}
	fmt.Printf("协议读取内容：%s\n", data)
	_, err = c.PutMultipart(ctx, "demo-bucket", "notes/big.txt",
		strings.NewReader("0123456789"), filex.PutOptions{}, 3, 2)
	if err != nil {
		return err
	}
	fmt.Println("分片上传完成")
	_, _ = c.SetBucketVersioning(ctx, "demo-bucket", true)
	_, _ = c.Put(ctx, "demo-bucket", "notes/versioned.txt",
		strings.NewReader("v1"), filex.PutOptions{})
	_, _ = c.Put(ctx, "demo-bucket", "notes/versioned.txt",
		strings.NewReader("v2"), filex.PutOptions{})
	versions, err := c.ListVersions(ctx, "demo-bucket", "notes/versioned.txt")
	if err != nil {
		return err
	}
	fmt.Printf("版本化演示：共 %d 个版本\n", len(versions))
	_ = srv.Close()
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "监听地址")
	dataDir := flag.String("data", "data", "数据目录")
	flag.Parse()
	if err := run(*addr, *dataDir); err != nil {
		log.Println(err)
		os.Exit(1)
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}
