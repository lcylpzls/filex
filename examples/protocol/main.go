// 协议端到端示例：启动自研协议服务端，用客户端完成桶与对象的全流程。
package main

import (
	"context"
	"encoding/hex"
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

func runWithOptions(addr, dataDir, token string, encKey []byte) error {
	store, err := filex.New(filex.Config{DataDir: dataDir, EncryptionKey: encKey})
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{
		Handler: server.NewHandler(server.HandlerConfig{Store: store, Token: token}),
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
	var opts []client.Option
	if token != "" {
		opts = append(opts, client.WithToken(token))
	}
	c, err := client.New("http://"+ln.Addr().String(), opts...)
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
	token := flag.String("token", "", "Bearer 令牌（可选）")
	encKeyHex := flag.String("enc-key", "", "32 字节主密钥（64 位十六进制，可选）")
	flag.Parse()
	var encKey []byte
	if *encKeyHex != "" {
		key, err := hex.DecodeString(*encKeyHex)
		if err != nil {
			log.Println("enc-key 必须是十六进制")
			os.Exit(1)
		}
		encKey = key
	}
	if err := runWithOptions(*addr, *dataDir, *token, encKey); err != nil {
		log.Println(err)
		os.Exit(1)
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}
