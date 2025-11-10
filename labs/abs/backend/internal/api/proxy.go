package api

import (
	"abs/internal/service"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ProxyToApp 代理请求到应用的实际端口
func ProxyToApp(appService *service.AppService) gin.HandlerFunc {
	return func(c *gin.Context) {
		appID := c.Param("app_id")
		path := c.Param("path")

		// 确保 path 以 / 开头
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		log.Printf("[PROXY] Request for app=%s, path=%s", appID, path)

		// 1. 查找应用
		app, err := appService.GetApp(appID)
		if err != nil {
			log.Printf("[PROXY] App not found: %s", appID)
			c.String(http.StatusNotFound, "Application not found")
			return
		}

		// 2. 检查应用是否运行
		if app.Status != "running" || app.RunningPID <= 0 {
			log.Printf("[PROXY] App not running: %s (status=%s, pid=%d)", appID, app.Status, app.RunningPID)
			c.String(http.StatusServiceUnavailable, "Application is not running. Please start it first.")
			return
		}

		// 3. 从 start_command 提取端口
		port := extractPortFromCommand(app.StartCommand)
		if port == 0 {
			log.Printf("[PROXY] Could not extract port from start_command: %v", app.StartCommand)
			c.String(http.StatusInternalServerError, "Application port not configured")
			return
		}

		// 4. 构建目标 URL
		targetURL := fmt.Sprintf("http://localhost:%d%s", port, path)
		log.Printf("[PROXY] Proxying to: %s", targetURL)

		// 5. 创建反向代理
		target, err := url.Parse(fmt.Sprintf("http://localhost:%d", port))
		if err != nil {
			log.Printf("[PROXY] Invalid target URL: %v", err)
			c.String(http.StatusInternalServerError, "Invalid proxy target")
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)

		// 自定义错误处理
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[PROXY] Proxy error for %s: %v", targetURL, err)
			http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
		}

		// 修改请求路径
		originalPath := c.Request.URL.Path
		c.Request.URL.Path = path
		c.Request.URL.RawPath = path

		// 执行代理
		proxy.ServeHTTP(c.Writer, c.Request)

		// 恢复原始路径（虽然不太需要，因为请求已经处理完）
		c.Request.URL.Path = originalPath
	}
}

// extractPortFromCommand 从启动命令中提取端口号
// 支持格式：
// - ["python3", "server.py", "--port", "8091"]
// - ["python3", "server.py", "--port=8091"]
// - ["node", "server.js", "-p", "3000"]
func extractPortFromCommand(command []string) int {
	if len(command) == 0 {
		return 0
	}

	for i := 0; i < len(command); i++ {
		arg := command[i]

		// 检查 --port 8091 格式
		if (arg == "--port" || arg == "-p" || arg == "--p") && i+1 < len(command) {
			if port, err := strconv.Atoi(command[i+1]); err == nil {
				return port
			}
		}

		// 检查 --port=8091 格式
		if strings.HasPrefix(arg, "--port=") {
			portStr := strings.TrimPrefix(arg, "--port=")
			if port, err := strconv.Atoi(portStr); err == nil {
				return port
			}
		}

		// 检查 -p=8091 格式
		if strings.HasPrefix(arg, "-p=") {
			portStr := strings.TrimPrefix(arg, "-p=")
			if port, err := strconv.Atoi(portStr); err == nil {
				return port
			}
		}
	}

	return 0
}
