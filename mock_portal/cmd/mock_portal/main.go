package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mock_portal/internal/catalog"
	"mock_portal/internal/diagnostics"
	"mock_portal/internal/httpapi"
	"mock_portal/internal/portalconfig"
	"mock_portal/internal/web"
)

func main() {
	configPath := flag.String("config", "portal.yaml", "门户配置文件路径")
	flag.Parse()

	log.SetFlags(log.LstdFlags)
	cfg, err := portalconfig.Load(*configPath)
	if err != nil {
		log.Fatalf("加载门户配置失败: %v", err)
	}
	cat, err := catalog.New(cfg.PresetsDir, cfg.ExcludePresets)
	if err != nil {
		log.Fatalf("初始化 presets 目录失败: %v", err)
	}
	diag := diagnostics.New(diagnostics.DefaultConfig())
	diag.Run(cat)
	defer diag.Stop()

	handler := httpapi.New(cat, diag, web.Handler()).Handler()
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		log.Printf("正在关闭 Mock 门户")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("关闭 HTTP 服务: %v", err)
		}
		close(done)
	}()

	log.Printf("Mock 门户监听 http://%s", cfg.Listen)
	log.Printf("presets 目录: %s", cfg.PresetsDir)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("启动门户失败: %v", err)
	}
	<-done
}
