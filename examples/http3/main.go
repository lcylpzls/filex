// HTTP/3 端到端示例命令：启动 webx 承载的 filex 协议服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/logx"
)

func main() {
	var (
		listen = flag.String("listen", "127.0.0.1:9443", "HTTP/3 监听地址")
		cert   = flag.String("cert", "", "TLS 证书文件（PEM）")
		key    = flag.String("key", "", "TLS 私钥文件（PEM）")
		token  = flag.String("token", "", "Bearer 令牌（可选）")
		data   = flag.String("data", "data", "数据目录")
	)
	flag.Parse()
	if *cert == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "用法：http3 -cert 证书 -key 私钥 [-listen 地址] [-token 令牌] [-data 目录]")
		os.Exit(2)
	}
	store, err := filex.New(filex.Config{DataDir: *data})
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化存储失败：%v\n", err)
		os.Exit(1)
	}
	defer store.Close()
	logger, err := logx.NewBuilder().EnableWriter(os.Stdout, logx.InfoLevel).Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败：%v\n", err)
		os.Exit(1)
	}
	s, _, err := startAndWait(context.Background(), serverConfig{
		Store: store,
		Token: *token,
	}, *cert, *key, *listen, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动服务失败：%v\n", err)
		os.Exit(1)
	}
	logger.Info("filex HTTP/3 服务已启动", logx.Fields(logx.String("地址", *listen)))
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	_ = s.Stop(context.Background())
}
