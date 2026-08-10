// HTTP/3 端到端示例服务端：webx 承载 filex 协议。
package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/server"
	"github.com/lcylpzls/logx"
	"github.com/lcylpzls/webx/v2"
)

type serverConfig struct {
	Store     *filex.Store
	Token     string
	OnRequest func(proto string)
}

// newHTTPS3Server 构造挂载 filex 协议的 webx HTTP/3 服务。
func newHTTPS3Server(cfg serverConfig, certFile, keyFile, listen string, logger logx.Logger) (*webx.Server, error) {
	filexHandler := server.NewHandler(server.HandlerConfig{Store: cfg.Store, Token: cfg.Token})
	s := webx.NewServer(webx.Config{
		TLSCertFile:     certFile,
		TLSKeyFile:      keyFile,
		ShutdownTimeout: 5 * time.Second,
	}, logger)
	s.UseHttp3Listen(listen)
	wrap := func(c *webx.Context) {
		if cfg.OnRequest != nil {
			cfg.OnRequest(c.Request().Proto)
		}
		filexHandler.ServeHTTP(c.Writer(), c.Request())
	}
	s.RegisterRoutes([]webx.Route{
		{Method: http.MethodGet, Path: "/*path", Handler: wrap},
		{Method: http.MethodPut, Path: "/*path", Handler: wrap},
		{Method: http.MethodPost, Path: "/*path", Handler: wrap},
		{Method: http.MethodDelete, Path: "/*path", Handler: wrap},
		{Method: http.MethodHead, Path: "/*path", Handler: wrap},
	})
	return s, nil
}

// startAndWait 启动服务并等待监听就绪。
func startAndWait(ctx context.Context, cfg serverConfig, certFile, keyFile, listen string, logger logx.Logger) (*webx.Server, string, error) {
	s, err := newHTTPS3Server(cfg, certFile, keyFile, listen, logger)
	if err != nil {
		return nil, "", err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.Start() }()
	for i := 0; i < 500; i++ {
		if addr := s.ListenerAddr(); addr != "" {
			return s, addr, nil
		}
		select {
		case err := <-errCh:
			return nil, "", err
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil, "", errors.New("服务启动超时")
}
