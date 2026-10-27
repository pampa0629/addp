package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/monitor/internal/models"
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

type platformAlertMessage struct {
	Schema      string                              `json:"schema"`
	EventID     string                              `json:"event_id"`
	EventType   string                              `json:"event_type"`
	IncidentID  uint                                `json:"incident_id"`
	Node        string                              `json:"node"`
	Signal      string                              `json:"signal"`
	InstanceID  string                              `json:"instance_id"`
	Severity    string                              `json:"severity"`
	OccurredAt  time.Time                           `json:"occurred_at"`
	SubjectKind string                              `json:"subject_kind"`
	SubjectID   string                              `json:"subject_id"`
	Test        bool                                `json:"test"`
	Evidence    *models.ServiceAvailabilityEvidence `json:"evidence"`
}

func wecomPlatformAlertPayload(raw, deliveryID string) (any, error) {
	var event platformAlertMessage
	if json.Unmarshal([]byte(raw), &event) != nil {
		return nil, ErrLogInvalid
	}
	content := ""
	if event.Schema == "addp.platform-alert-notification-test/v1" && event.Test {
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
	} else if event.Schema == "addp.platform-resource-alert/v1" && event.SubjectKind == "host_node" && event.SubjectID != "" && event.EventID != "" && event.IncidentID != 0 && !event.OccurredAt.IsZero() {
		label := map[string]string{"opened": "资源压力告警", "resolved": "资源压力恢复"}[event.EventType]
		signal := map[string]string{"node.cpu.busy_percent": "CPU 忙碌率", "node.memory.used_percent": "内存使用率"}[event.Signal]
		var capacity *models.ResourceAlertEvidence
		if strings.HasPrefix(event.Signal, models.FilesystemCapacityMetric+":") {
			var saved struct {
				Evidence *models.ResourceAlertEvidence `json:"evidence"`
			}
			if json.Unmarshal([]byte(raw), &saved) != nil || !validFilesystemAlertEvidence(event.Signal, saved.Evidence) {
				return nil, ErrLogInvalid
			}
			capacity = saved.Evidence
			signal = "文件系统可用容量使用率"
		}
		if label == "" || signal == "" || event.Severity != "warning" {
			return nil, ErrLogInvalid
		}
		content = fmt.Sprintf("**ADDP 平台主机：%s**\n> 主机 ID：%s\n> 指标：%s\n> 事件发生时间（UTC）：%s\n> 告警 ID：%d\n> 事件 ID：%s", label, wecomText(event.SubjectID), signal, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.IncidentID, wecomText(event.EventID))
		if capacity != nil {
			f := capacity.Filesystem
			content += fmt.Sprintf("\n> 挂载：%s / %s / %s\n> 使用率：%.2f%%\n> 总容量：%s\n> 已用：%s\n> 普通用户可用：%s", wecomText(f.Dimensions["device"]), wecomText(f.Dimensions["mountpoint"]), wecomText(f.Dimensions["fstype"]), capacity.Latest, filesystemCapacityIEC(f.TotalBytes), filesystemCapacityIEC(f.UsedBytes), filesystemCapacityIEC(f.AvailableBytes))
		}
	} else if event.Schema == "addp.platform-process-resource-alert/v1" && event.SubjectKind == "module_instance" && event.SubjectID != "" && event.InstanceID != "" && event.EventID != "" && event.IncidentID != 0 && !event.OccurredAt.IsZero() {
		label := map[string]string{"opened": "资源超限告警", "resolved": "资源告警恢复"}[event.EventType]
		if label == "" || event.Severity != "warning" {
			return nil, ErrLogInvalid
		}
		content = fmt.Sprintf("**ADDP 服务实例：%s**\n> 实例：%s\n> 登记 ID：%s\n> 事件发生时间（UTC）：%s\n> 告警 ID：%d\n> 事件 ID：%s", label, wecomText(event.InstanceID), wecomText(event.SubjectID), event.OccurredAt.UTC().Format(time.RFC3339Nano), event.IncidentID, wecomText(event.EventID))
	} else if event.Schema == "addp.platform-service-availability-alert/v1" && event.SubjectKind == "service_group" && event.EventID != "" && event.IncidentID != 0 && !event.OccurredAt.IsZero() {
		label := map[string]string{"opened": "服务组失联告警", "resolved": "服务组恢复"}[event.EventType]
		if label == "" || event.Severity != "warning" || !validAvailabilityEvidence(event.Evidence, event.SubjectID, event.Signal) {
			return nil, ErrLogInvalid
		}
		e := event.Evidence
		content = fmt.Sprintf("**ADDP 服务组：%s**\n> 规则：%s\n> 模块：%s\n> 角色／用途：%s / %s\n> 在线副本：%d\n> 事件发生时间（UTC）：%s\n> 告警 ID：%d\n> 事件 ID：%s", label, wecomText(e.RuleName), wecomText(e.ModuleName), wecomText(e.Role), wecomText(e.RuntimeName), e.OnlineReplicas, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.IncidentID, wecomText(event.EventID))
	} else if event.Schema == "addp.platform-http-alert/v1" && event.SubjectKind == "module" && event.SubjectID != "" && event.EventID != "" && event.IncidentID != 0 && !event.OccurredAt.IsZero() {
		label := map[string]string{"opened": "接口质量告警", "resolved": "接口质量恢复"}[event.EventType]
		if label == "" || event.Severity != "warning" {
			return nil, ErrLogInvalid
		}
		content = fmt.Sprintf("**ADDP 平台接口：%s**\n> 模块：%s\n> 事件发生时间（UTC）：%s\n> 告警 ID：%d\n> 事件 ID：%s", label, wecomText(event.SubjectID), event.OccurredAt.UTC().Format(time.RFC3339Nano), event.IncidentID, wecomText(event.EventID))
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
