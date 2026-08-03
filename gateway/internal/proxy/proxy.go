package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

type ServiceProxy struct {
	targetURL string
	proxy     *httputil.ReverseProxy
}

func NewServiceProxy(targetURL string) *ServiceProxy {
	target, err := url.Parse(targetURL)
	if err != nil {
		panic("invalid service proxy target: " + err.Error())
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	reverseProxy.Transport = transport
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, request *http.Request, proxyErr error) {
		log.Printf("代理请求失败: %v (target: %s)", proxyErr, targetURL)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"Service unavailable"}`))
	}
	return &ServiceProxy{
		targetURL: targetURL,
		proxy:     reverseProxy,
	}
}

// GetTargetURL 获取目标服务 URL
func (p *ServiceProxy) GetTargetURL() string {
	return p.targetURL
}

func (p *ServiceProxy) Handle(c *gin.Context) {
	p.proxy.ServeHTTP(c.Writer, c.Request)
}
