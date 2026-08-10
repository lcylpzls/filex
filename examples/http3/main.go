// HTTP/3 端到端示例命令：启动 webx 承载的 filex 协议服务。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/lcylpzls/clix"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/logx"
)

func main() {
	app, err := clix.New("filex-http3", "0.22.0",
		clix.WithDescription("filex HTTP/3 端到端示例"),
		clix.WithIO(os.Stdout, os.Stderr),
		clix.WithGlobalFlags(
			clix.StringFlag("listen", "HTTP/3 监听地址").Default("127.0.0.1:9443"),
			clix.StringFlag("cert", "TLS 证书文件（PEM）").Required(),
			clix.StringFlag("key", "TLS 私钥文件（PEM）").Required(),
			clix.StringFlag("token", "Bearer 令牌（可选）"),
			clix.StringFlag("data", "数据目录").Default("data"),
		),
		clix.WithRootAction(runHTTP3),
	)
	if err != nil {
		panic(err)
	}
	os.Exit(app.Execute(context.Background(), os.Args[1:]))
}

// runHTTP3 启动 filex HTTP/3 服务（clix 根 Action）。
func runHTTP3(ctx context.Context, c *clix.Context) error {
	store, err := filex.New(filex.Config{DataDir: c.GlobalString("data")})
	if err != nil {
		return err
	}
	defer store.Close()
	logger, err := logx.NewBuilder().EnableWriter(os.Stdout, logx.InfoLevel).Build()
	if err != nil {
		return err
	}
	s, _, err := startAndWait(context.Background(), serverConfig{
		Store: store,
		Token: c.GlobalString("token"),
	}, c.GlobalString("cert"), c.GlobalString("key"), c.GlobalString("listen"), logger)
	if err != nil {
		return err
	}
	logger.Info("filex HTTP/3 服务已启动", logx.Fields(logx.String("地址", c.GlobalString("listen"))))
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	return s.Stop(ctx)
}
