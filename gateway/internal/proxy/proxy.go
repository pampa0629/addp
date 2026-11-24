package proxy

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/addp/common/httptrace"
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
	reverseProxy.Transport = httptrace.Transport(transport, targetURL)
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

// Fresh connections make a partition detectable before any request bytes are sent.
// Sharing this transport retains the normal proxy/TLS settings without reusing idle sockets.
var readConnectTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true
	t.DisableKeepAlives = true
	dial := t.DialContext
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return dial(ctx, network, address)
	}
	return t
}()

func (p *ServiceProxy) HandleReadWithAlternate(c *gin.Context, alternate func() *ServiceProxy) {
	first := *p.proxy
	first.Transport = httptrace.Transport(readConnectTransport, p.targetURL)
	first.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" && c.Request.Context().Err() == nil && !c.Writer.Written() {
			if next := alternate(); next != nil {
				second := *next.proxy
				second.Transport = httptrace.Transport(readConnectTransport, next.targetURL)
				second.ServeHTTP(w, c.Request)
				return
			}
		}
		p.proxy.ErrorHandler(w, r, err)
	}
	first.ServeHTTP(c.Writer, c.Request)
}
