package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/router"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()
	commonConfig.InitLogger("gateway.log", nil)

	// 加载配置
	cfg := config.Load()

	// 检查端口是否可用
	if err := commonConfig.CheckPortAvailable(cfg.Port); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "port", cfg.Port)

	// 设置 Gin 模式
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	runtimeContext, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	listener, err := net.Listen("tcp", cfg.Port)
	if err != nil {
		logger.L().Error("Gateway 监听失败", "error", err)
		return
	}
	r, systemClient := router.SetupRouter(cfg)
	server := &http.Server{Handler: r}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()

	serviceURL := commonConfig.BuildServiceURL(commonConfig.GetServiceHost(), cfg.Port)
	registration := systemClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
		ModuleName: "gateway", Role: commonClient.ModuleRuntimeRoleIngress,
		ModuleURL: serviceURL, HealthCheckURL: serviceURL + "/health/ready",
	})
	modulelifecycle.CancelRuntimeOnFatal(registration, cancel)
	logger.L().Info("Gateway 服务启动", "addr", cfg.Port)
	select {
	case <-runtimeContext.Done():
	case err := <-serverDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L().Error("Gateway 服务异常退出", "error", err)
		}
		cancel()
	}
	shutdownContext, stopShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.L().Error("Gateway 服务关闭失败", "error", err)
	}
	<-registration.Done()
}
