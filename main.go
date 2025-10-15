package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var configFile = flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 根据public配置决定绑定地址
	var addr string
	if config.Server.Public {
		addr = fmt.Sprintf(":%d", config.Server.Port)
		log.Infof("服务器地址监听: 0.0.0.0:%d", config.Server.Port)
	} else {
		addr = fmt.Sprintf("127.0.0.1:%d", config.Server.Port)
		log.Infof("服务器地址监听: 127.0.0.1:%d", config.Server.Port)
	}

	server := &http.Server{Addr: addr, Handler: NewProxyHandler(config)}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("服务器关闭")
}
