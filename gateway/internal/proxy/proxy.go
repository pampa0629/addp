package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type ServiceProxy struct {
	targetURL string
	client    *http.Client
}

func NewServiceProxy(targetURL string) *ServiceProxy {
	return &ServiceProxy{
		targetURL: targetURL,
		client:    &http.Client{},
	}
}

func (p *ServiceProxy) Handle(c *gin.Context) {
	// 构建目标 URL
	path := c.Request.URL.Path
	targetURL := p.targetURL + path

	// 保留查询参数
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	// 读取请求体
	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, _ = io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// 创建新请求
	req, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("创建请求失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create proxy request",
		})
		return
	}

	// 复制请求头
	for key, values := range c.Request.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// 发送请求
	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("代理请求失败: %v (target: %s)", err, targetURL)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "Service unavailable",
			"service": p.targetURL,
		})
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	// 复制响应体
	c.Status(resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	c.Writer.Write(respBody)
}

// HandleWithPathRewrite 处理带路径重写的代理
func (p *ServiceProxy) HandleWithPathRewrite(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 移除路径前缀
		newPath := strings.TrimPrefix(c.Request.URL.Path, prefix)
		c.Request.URL.Path = newPath
		p.Handle(c)
	}
}