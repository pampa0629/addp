package scanflow

import (
	"errors"
	"fmt"
	"strings"
)

const MaxFailedTargetSamples = 20

type FailedTargetSample struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

type FailedTargetsError struct {
	Count   int                  `json:"failed_targets_count"`
	Samples []FailedTargetSample `json:"failed_target_samples"`
	targets map[string]struct{}
}

func (e *FailedTargetsError) Error() string {
	if e == nil || e.Count == 0 {
		return ""
	}
	message := fmt.Sprintf("%d 个扫描目标失败", e.Count)
	if len(e.Samples) == 0 {
		return message
	}
	first := e.Samples[0]
	if first.Target == "" {
		return fmt.Sprintf("%s: %s", message, first.Message)
	}
	return fmt.Sprintf("%s，首个失败目标 %s: %s", message, first.Target, first.Message)
}

type FailedTargetCollector struct {
	targets map[string]struct{}
	samples []FailedTargetSample
}

func (c *FailedTargetCollector) Add(target string, err error) {
	if c == nil || err == nil {
		return
	}
	var failed *FailedTargetsError
	if errors.As(err, &failed) {
		if len(failed.targets) > 0 {
			for _, sample := range failed.Samples {
				c.addSample(sample)
			}
			for nestedTarget := range failed.targets {
				c.addTarget(nestedTarget)
			}
			return
		}
		before := c.Count()
		for _, sample := range failed.Samples {
			c.addSample(sample)
		}
		merged := c.Count() - before
		for merged < failed.Count {
			c.addTarget(fmt.Sprintf("<unsampled-%p-%d>", failed, merged+1))
			merged++
		}
		return
	}

	c.addSample(FailedTargetSample{
		Target:  boundedFailureText(target, 512),
		Message: boundedFailureText(err.Error(), 512),
	})
}

func (c *FailedTargetCollector) addSample(sample FailedTargetSample) {
	if c == nil {
		return
	}
	sample.Target = boundedFailureText(sample.Target, 512)
	sample.Message = boundedFailureText(sample.Message, 512)
	if !c.addTarget(sample.Target) || len(c.samples) >= MaxFailedTargetSamples {
		return
	}
	c.samples = append(c.samples, sample)
}

func (c *FailedTargetCollector) addTarget(target string) bool {
	if c.targets == nil {
		c.targets = make(map[string]struct{})
	}
	if _, exists := c.targets[target]; exists {
		return false
	}
	c.targets[target] = struct{}{}
	return true
}

func (c *FailedTargetCollector) Count() int {
	if c == nil {
		return 0
	}
	return len(c.targets)
}

func (c *FailedTargetCollector) Err() error {
	if c == nil || c.Count() == 0 {
		return nil
	}
	samples := append([]FailedTargetSample(nil), c.samples...)
	targets := make(map[string]struct{}, len(c.targets))
	for target := range c.targets {
		targets[target] = struct{}{}
	}
	return &FailedTargetsError{Count: len(targets), Samples: samples, targets: targets}
}

func FailedTargetDetails(err error) (int, []FailedTargetSample) {
	var failed *FailedTargetsError
	if !errors.As(err, &failed) || failed == nil {
		return 0, nil
	}
	return failed.Count, append([]FailedTargetSample(nil), failed.Samples...)
}

func boundedFailureText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
