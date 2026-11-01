package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	commoninference "github.com/addp/common/inference"
)

// ChatStream owns one upstream response and one resolved model snapshot.
type ChatStream struct {
	response *http.Response
	resolved *resolvedModel
}

func (s *Runtime) OpenChatStream(ctx context.Context, req commoninference.ChatRequest) (*ChatStream, error) {
	if !req.Stream {
		return nil, ErrInvalidRequest
	}
	resolved, body, err := s.prepareChat(ctx, req)
	if err != nil {
		return nil, err
	}
	endpoint, err := joinEndpoint(resolved.provider.Endpoint, "chat/completions")
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if resolved.credential != "" {
		request.Header.Set("Authorization", "Bearer "+resolved.credential)
	}
	response, err := s.client.Do(request)
	if err != nil {
		if upstreamTimedOut(ctx, err) {
			return nil, ErrTimeout
		}
		return nil, ErrUpstreamUnavailable
	}
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode < 200 || response.StatusCode >= 300 || media != "text/event-stream" {
		defer response.Body.Close()
		// Drain only a bounded error body; it is never returned or logged.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if response.StatusCode == http.StatusGatewayTimeout {
			return nil, ErrTimeout
		}
		if response.StatusCode >= 500 {
			return nil, ErrUpstreamUnavailable
		}
		return nil, ErrUpstreamFailed
	}
	return &ChatStream{response: response, resolved: resolved}, nil
}

func (s *ChatStream) Close() error { return s.response.Body.Close() }

// Consume emits only validated public text. A successful finish and [DONE] are both required.
func (s *ChatStream) Consume(ctx context.Context, emit func(commoninference.ChatStreamEvent) error) error {
	sequence := 0
	send := func(event commoninference.ChatStreamEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sequence++
		event.SchemaVersion, event.Sequence = commoninference.ChatStreamSchemaVersion, sequence
		return emit(event)
	}
	if err := send(commoninference.ChatStreamEvent{Type: "start", DeploymentID: s.resolved.deployment.ID, ProfileVersion: int64(s.resolved.profile.Version)}); err != nil {
		return err
	}
	usage := commoninference.Usage{}
	finished, textBytes := false, 0
	consume := func(data string) (bool, error) {
		if !utf8.ValidString(data) {
			return false, ErrUpstreamFailed
		}
		if data == "[DONE]" {
			if !finished || textBytes == 0 {
				return false, ErrUpstreamFailed
			}
			return true, send(commoninference.ChatStreamEvent{Type: "completed", Usage: &usage})
		}
		var chunk struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role         string            `json:"role"`
					Content      *string           `json:"content"`
					ToolCalls    []json.RawMessage `json:"tool_calls"`
					FunctionCall *json.RawMessage  `json:"function_call"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Total      int `json:"total_tokens"`
			} `json:"usage"`
			Error *json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || chunk.Error != nil {
			return false, ErrUpstreamFailed
		}
		if chunk.Usage != nil {
			if chunk.Usage.Prompt < 0 || chunk.Usage.Completion < 0 || chunk.Usage.Total < 0 {
				return false, ErrUpstreamFailed
			}
			usage = commoninference.Usage{InputTokens: chunk.Usage.Prompt, OutputTokens: chunk.Usage.Completion, TotalTokens: chunk.Usage.Total}
		}
		if len(chunk.Choices) > 1 {
			return false, ErrUpstreamFailed
		}
		for _, choice := range chunk.Choices {
			if finished || choice.Index != 0 || (choice.Delta.Role != "" && choice.Delta.Role != "assistant") || len(choice.Delta.ToolCalls) > 0 || choice.Delta.FunctionCall != nil {
				return false, ErrUpstreamFailed
			}
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				textBytes += len(*choice.Delta.Content)
				if textBytes > 2<<20 {
					return false, ErrUpstreamFailed
				}
				if err := send(commoninference.ChatStreamEvent{Type: "text_delta", Delta: *choice.Delta.Content}); err != nil {
					return false, err
				}
			}
			if choice.FinishReason != nil {
				if *choice.FinishReason != "stop" {
					return false, ErrUpstreamFailed
				}
				finished = true
			}
		}
		return false, nil
	}
	scanner := bufio.NewScanner(io.LimitReader(s.response.Body, 32<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	var lines []string
	frameBytes := 0
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		frameBytes += len(line) + 1
		if frameBytes > 512<<10 {
			return ErrUpstreamFailed
		}
		if line == "" {
			frameBytes = 0
		}
		if line == "" && len(lines) > 0 {
			done, err := consume(strings.Join(lines, "\n"))
			lines = nil
			if err != nil || done {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() == context.Canceled {
		return context.Canceled
	}
	if upstreamTimedOut(ctx, scanner.Err()) {
		return ErrTimeout
	}
	return fmt.Errorf("%w: incomplete chat stream", ErrUpstreamFailed)
}
