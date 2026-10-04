package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const wecomEndpoint = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"

var (
	wecomKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	ErrWeComRejected    = errors.New("wecom rejected notification")
	ErrWeComRateLimited = errors.New("wecom notification rate limited")
	ErrWeComResponse    = errors.New("wecom response invalid")
	ErrWeComNetwork     = errors.New("wecom network unavailable")
	ErrWeComHTTP        = errors.New("wecom HTTP request failed")
)

type WeComTransport interface {
	SendWeComMessage(context.Context, WebhookMessage, string, time.Time) (WebhookSendResult, error)
}

// Only the fixed official endpoint is accepted; the token never becomes an ordinary URL field.
func wecomKey(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "qyapi.weixin.qq.com" || u.Path != "/cgi-bin/webhook/send" || u.RawPath != "" || u.User != nil || strings.Contains(raw, "#") || u.ForceQuery {
		return "", ErrLogInvalid
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["key"]) != 1 || !wecomKeyPattern.MatchString(q.Get("key")) {
		return "", ErrLogInvalid
	}
	return q.Get("key"), nil
}

func (s *HTTPWebhookSender) SendWeComMessage(ctx context.Context, message WebhookMessage, key string, _ time.Time) (WebhookSendResult, error) {
	if !wecomKeyPattern.MatchString(key) || message.RequestURL != wecomEndpoint {
		return WebhookSendResult{}, ErrLogInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, s.client.Timeout)
	defer cancel()
	body, err := json.Marshal(message.Payload)
	if err != nil {
		return WebhookSendResult{}, ErrLogInvalid
	}
	// The domain is fixed. The shared transport validates resolved IPs at dial time.
	result, err := s.postJSON(ctx, wecomEndpoint+"?key="+url.QueryEscape(key), body, http.Header{}, checkWeComResponse)
	if err != nil && !errors.Is(err, ErrWeComRejected) && !errors.Is(err, ErrWeComRateLimited) && !errors.Is(err, ErrWeComResponse) {
		if result.HTTPStatus != 0 {
			return result, ErrWeComHTTP
		}
		return result, ErrWeComNetwork
	}
	return result, err
}

func checkWeComResponse(reader io.Reader) error {
	body, err := io.ReadAll(io.LimitReader(reader, 4097))
	var response struct {
		Code *int `json:"errcode"`
	}
	if err != nil || len(body) > 4096 || json.Unmarshal(body, &response) != nil || response.Code == nil {
		return ErrWeComResponse
	}
	if *response.Code == 45009 {
		return ErrWeComRateLimited
	}
	if *response.Code != 0 {
		return ErrWeComRejected
	}
	return nil
}

type platformLogMessage struct {
	Schema     string    `json:"schema"`
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	IncidentID uint      `json:"incident_id"`
	Node       string    `json:"node"`
	Signal     string    `json:"signal"`
	InstanceID string    `json:"instance_id"`
	Severity   string    `json:"severity"`
	OccurredAt time.Time `json:"occurred_at"`
	Test       bool      `json:"test"`
}

func wecomLogPayload(raw, deliveryID string) (any, error) {
	var event platformLogMessage
	if json.Unmarshal([]byte(raw), &event) != nil {
		return nil, ErrLogInvalid
	}
	content := ""
	if event.Schema == "addp.platform-log-notification-test/v1" && event.Test {
		content = "**ADDP 平台告警接入测试（无需处理）**\n企业微信通知渠道连接测试。"
	} else if event.Schema == "addp.platform-log-alert/v1" && event.EventID != "" && event.IncidentID != 0 && !event.OccurredAt.IsZero() {
		label := map[string]string{"opened": "异常告警", "resolved": "告警恢复"}[event.EventType]
		if label == "" {
			return nil, ErrLogInvalid
		}
		severity := map[string]string{"critical": "严重", "warning": "警告"}[event.Severity]
		if severity == "" {
			return nil, ErrLogInvalid
		}
		content = fmt.Sprintf("**ADDP 平台日志链路：%s**\n> 节点：%s\n> 原因：%s\n> 严重级别：%s\n> 事件发生时间（UTC）：%s\n> 告警 ID：%d\n> 事件 ID：%s", label, wecomText(event.Node), wecomSignalLabel(event.Signal), severity, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.IncidentID, wecomText(event.EventID))
		if event.InstanceID != "" {
			content += "\n> 实例 ID：" + wecomText(event.InstanceID)
		}
		content += "\n\n此消息记录原事件，当前状态请查看平台日志链路。"
	} else {
		return nil, ErrLogInvalid
	}
	content += "\n> 投递 ID：" + wecomText(deliveryID)
	if len(content) > 4096 {
		return nil, ErrLogInvalid
	}
	return map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": content}}, nil
}

func wecomText(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ", "*", "", "`", "", "[", "", "]", "", "(", "", ")", "").Replace(value)
}

func wecomSignalLabel(signal string) string {
	label := map[string]string{
		"observation_missing":   "观测器失联",
		"log_api":               "日志查询入口不可用",
		"delivery_probe":        "端到端投递失败",
		"delivery_delay":        "投递延迟升高",
		"collector_metrics":     "采集器指标不可用",
		"collector_dropped":     "采集器新增丢弃",
		"collector_retries":     "采集器持续重试",
		"source_observation":    "日志源观测失败",
		"housekeeping":          "源清理观测异常",
		"source_quota":          "源容量耗尽或清理失败",
		"source_capacity":       "源容量接近额度",
		"receiver_missing":      "在线实例的接收器观测缺失",
		"receiver_loss":         "接收器新增丢弃或写入失败",
		"source_early_cleaning": "源文件提前清理风险",
	}[signal]
	if label == "" {
		return wecomText(signal)
	}
	return label
}
