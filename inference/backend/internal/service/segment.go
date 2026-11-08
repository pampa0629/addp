package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	ci "github.com/addp/common/inference"
)

func localSegmentEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "http" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") && net.ParseIP(u.Hostname()).IsLoopback()
}

var segmentTransport = &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4, IdleConnTimeout: time.Minute}

func (s *Runtime) Segment(ctx context.Context, req ci.SegmentRequest) (*ci.SegmentResponse, error) {
	if req.SchemaVersion != ci.SchemaVersion || req.TenantID == 0 || len(req.Scope) != 64 {
		return nil, ErrInvalidRequest
	}
	if _, err := hex.DecodeString(req.Scope); err != nil {
		return nil, ErrInvalidRequest
	}
	switch req.Action {
	case "prepare":
		if req.SessionID != "" || req.Prompt != nil || len(req.Image) == 0 || len(req.Image) > 7<<20 {
			return nil, ErrInvalidRequest
		}
	case "prompt", "release":
		if len(req.SessionID) != 43 || req.Image != "" || (req.Action == "prompt") != (req.Prompt != nil) {
			return nil, ErrInvalidRequest
		}
	default:
		return nil, ErrInvalidRequest
	}
	r, err := s.resolve(ctx, req.TenantID, req.ModelProfileID, ci.OperationSegmentation, ci.ModalityImage)
	if err != nil {
		return nil, err
	}
	if r.provider.AdapterType != AdapterLocalSegment || !localSegmentEndpoint(r.provider.Endpoint) || r.credential == "" {
		return nil, ErrUnsupported
	}
	scope := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%d/%s/%s", req.TenantID, r.profile.ID, r.profile.Version, r.deployment.ID, req.Scope)))
	body := map[string]any{"action": req.Action, "model": r.deployment.UpstreamModel, "scope": fmt.Sprintf("%x", scope), "session_id": req.SessionID, "image": req.Image, "prompt": req.Prompt}
	local := *s
	local.client = &http.Client{Timeout: 2 * time.Minute, Transport: segmentTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var out ci.SegmentResponse
	if err := local.invoke(ctx, r, "segment", body, &out); err != nil {
		return nil, err
	}
	if len(out.SessionID) != 43 || (req.SessionID != "" && req.SessionID != out.SessionID) || (req.Action != "release" && (out.Width <= 0 || out.Width > 1024 || out.Height <= 0 || out.Height > 1024)) || len(out.Masks) > 64 {
		return nil, ErrUpstreamFailed
	}
	for _, m := range out.Masks {
		if m.X < 0 || m.Y < 0 || m.Width < 1 || m.Height < 1 || m.X > out.Width-m.Width || m.Y > out.Height-m.Height || m.PNG == "" {
			return nil, ErrUpstreamFailed
		}
	}
	out.ProfileVersion = int64(r.profile.Version)
	out.DeploymentID = r.deployment.ID
	return &out, nil
}
