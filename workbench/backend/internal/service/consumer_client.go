package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/addp/workbench/internal/models"
)

const maxConsumerDescriptorBytes = 1 << 20

type DescriptorRequest struct {
	BearerToken    string
	AcceptLanguage string
	RequestID      string
	Ref            models.ServiceReference
}

type DescriptorReader interface {
	GetDescriptor(context.Context, DescriptorRequest) (*models.ConsumerDescriptor, error)
}

type HTTPDescriptorReader struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPDescriptorReader(baseURL string, httpClient *http.Client) (*HTTPDescriptorReader, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Service URL")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HTTPDescriptorReader{baseURL: parsed.String(), httpClient: httpClient}, nil
}

func (r *HTTPDescriptorReader) GetDescriptor(ctx context.Context, input DescriptorRequest) (*models.ConsumerDescriptor, error) {
	if input.Ref.ServiceType != "query" || input.Ref.ServiceID <= 0 || strings.TrimSpace(input.BearerToken) == "" {
		return nil, ErrInvalidDataApplication
	}
	path := r.baseURL + "/api/v1/service/consumer/services/" + url.PathEscape(input.Ref.ServiceType) + "/" + strconv.FormatInt(input.Ref.ServiceID, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("build Service descriptor request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+input.BearerToken)
	if input.AcceptLanguage != "" {
		request.Header.Set("Accept-Language", input.AcceptLanguage)
	}
	if input.RequestID != "" {
		request.Header.Set("X-Request-ID", input.RequestID)
	}
	response, err := r.httpClient.Do(request)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
		return nil, ErrServiceAccessDenied
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrServiceUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxConsumerDescriptorBytes+1))
	decoder.DisallowUnknownFields()
	var descriptor models.ConsumerDescriptor
	if err := decoder.Decode(&descriptor); err != nil {
		return nil, ErrServiceUnavailable
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, ErrServiceUnavailable
	}
	return &descriptor, nil
}
