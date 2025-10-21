package embedding

import (
	"errors"
	"fmt"
	"time"
)

// ServiceConfig 描述在线推理服务的访问参数
type ServiceConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Models  map[Modality]string
}

// Validate 对配置进行合法性校验
func (c ServiceConfig) Validate() error {
	if c.BaseURL == "" {
		return errors.New("embedding service base url missing")
	}
	if len(c.Models) == 0 {
		return errors.New("embedding service models not configured")
	}
	return nil
}

// ModelFor 获取某个模态的默认模型
func (c ServiceConfig) ModelFor(m Modality) (string, error) {
	if model, ok := c.Models[m]; ok && model != "" {
		return model, nil
	}
	return "", fmt.Errorf("model for modality %s not configured", m)
}

// MustModelFor 获取模型，不存在时 panic，适用于初始化阶段
func (c ServiceConfig) MustModelFor(m Modality) string {
	model, err := c.ModelFor(m)
	if err != nil {
		panic(err)
	}
	return model
}
