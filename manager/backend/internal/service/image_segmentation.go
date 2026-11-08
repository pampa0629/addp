package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	commonclient "github.com/addp/common/client"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	ci "github.com/addp/common/inference"
	protection "github.com/addp/manager/internal/protection"
	"strconv"
)

const ImageSegmentationScenario = "image_segmentation"

var ErrSegmentBinding = errors.New("segmentation profile not configured")
var ErrSegmentBusy = errors.New("segmentation window preparation busy")
var ErrSegmentUpstream = errors.New("image segmentation inference failed")

type ImageSegmentInput struct {
	Locator   string            `json:"locator" binding:"required"`
	Window    *ImageWindow      `json:"window"`
	Action    string            `json:"action" binding:"required"`
	SessionID string            `json:"session_id,omitempty"`
	Prompt    *ci.SegmentPrompt `json:"prompt,omitempty"`
}
type ImageSegmentResult struct {
	SessionID string            `json:"session_id"`
	Window    *ImageWindow      `json:"window,omitempty"`
	Image     string            `json:"image,omitempty"`
	Masks     []ci.InstanceMask `json:"masks,omitempty"`
}
type segmentClient interface {
	Segment(context.Context, ci.SegmentRequest) (*ci.SegmentResponse, error)
}
type ImageSegmentationService struct {
	source       *commonclient.SystemServiceClient
	metadata     *MetadataService
	protection   *projectionstore.Store
	binding      *InferenceScenarioBindingService
	inference    segmentClient
	prepareSlots chan struct{}
}

func NewImageSegmentationService(meta *MetadataService, store *projectionstore.Store, binding *InferenceScenarioBindingService, client segmentClient, source *commonclient.SystemServiceClient) *ImageSegmentationService {
	return &ImageSegmentationService{source, meta, store, binding, client, make(chan struct{}, 1)}
}
func (s *ImageSegmentationService) Run(ctx context.Context, tenant uint, identity, credential string, in ImageSegmentInput) (*ImageSegmentResult, error) {
	if tenant == 0 || identity == "" || credential == "" {
		return nil, ErrEngineAccessDenied
	}
	if in.Action != "prepare" && in.Action != "prompt" && in.Action != "release" {
		return nil, ErrSegmentImageUnsupported
	}
	check := func() (*StorageDataItem, string, error) {
		target, item, err := s.metadata.resolveStorageDataItem(ctx, in.Locator, &tenant)
		if err != nil {
			return nil, "", err
		}
		tiffGroup := target.Descriptor.Format == "tiff" && target.Descriptor.Layout == "multi" && len(target.Descriptor.Refs) == 1 && target.Descriptor.Refs[0].Primary && target.Descriptor.Refs[0].Required && target.Descriptor.Refs[0].Path == target.StorageRef
		if (!tiffGroup && target.Descriptor.Layout != "single") || target.Descriptor.DataType != "media" {
			return nil, "", ErrSegmentImageUnsupported
		}
		switch target.Descriptor.Format {
		case "jpeg", "tiff":
		default:
			return nil, "", ErrSegmentImageUnsupported
		}
		if err := protection.RequireUnmanagedDataItem(ctx, s.protection, tenant, target.ItemFingerprint, time.Now().UTC()); err != nil {
			return nil, "", ErrEngineAccessDenied
		}
		path, _, err := streamStorageRefPath(target.EngineType, target.EngineID, target.StorageRef)
		if err != nil || s.source == nil {
			return nil, "", ErrEngineAccessDenied
		}
		boundary, err := authorizationfacts.NewScannedFileCompositionBoundary(target.Descriptor.Format, string(target.Descriptor.Layout), []authorizationfacts.FileCompositionRef{{Target: path, Role: "image", Required: true, Primary: true}})
		if err != nil {
			return nil, "", ErrEngineAccessDenied
		}
		if err = s.source.CheckManagerFileRead(ctx, credential, commonclient.ManagerFileExpectedSource{EngineVersion: strconv.FormatInt(target.engine.Version, 10), FileComposition: *boundary}); err != nil {
			return nil, "", err
		}
		pl, pluginErr := plugin.Get(target.EngineType)
		if pluginErr != nil {
			return nil, "", ErrSegmentImageUnsupported
		}
		stat, ok := pl.(plugin.EngineCatalogFactsProvider)
		if !ok {
			return nil, "", ErrSegmentImageUnsupported
		}
		fact, err := streamEngineCatalogFacts(ctx, stat, plugin.ConnectionInfo(target.engine.ConnectionInfo), path, target.StorageRef)
		if err != nil || fact == nil || (fact.ETag == "" && fact.ModifiedAt.IsZero()) {
			return nil, "", ErrSegmentImageUnsupported
		}
		revision, err := json.Marshal([]any{tenant, identity, target.ItemFingerprint, item.ID, target.engine.Version, item.DataUpdatedAt, item.ScannedAt, item.ObjectSizeBytes, fact.ModifiedAt, fact.Size, fact.ETag, in.Window})
		if err != nil {
			return nil, "", err
		}
		return target, fmt.Sprintf("%x", sha256.Sum256(revision)), nil
	}
	target, scope, err := check()
	if err != nil {
		return nil, err
	}
	binding, err := s.binding.ResolveScenario(ctx, tenant, ImageSegmentationScenario)
	if err != nil {
		return nil, ErrSegmentBinding
	}
	req := ci.SegmentRequest{SchemaVersion: ci.SchemaVersion, TenantID: tenant, ModelProfileID: binding.ModelProfileID, Scope: scope, Action: in.Action, SessionID: in.SessionID, Prompt: in.Prompt}
	result := new(ImageSegmentResult)
	if in.Action == "prepare" {
		select {
		case s.prepareSlots <- struct{}{}:
			defer func() { <-s.prepareSlots }()
		default:
			return nil, ErrSegmentBusy
		}
		// Same controlled source path as storage preview. Validate the budget before opening.
		reader, _, _, _, err := s.metadata.streamStorageContent(ctx, target.engine, target.StorageRef, "", maxSegmentSourceBytes, func() error {
			_, current, err := check()
			if err != nil {
				return err
			}
			if current != scope {
				return ErrEngineAccessDenied
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		encoded, w, err := renderSegmentWindow(reader, target.Descriptor.Format, in.Window)
		if err != nil {
			return nil, err
		}
		req.Image = base64.StdEncoding.EncodeToString(encoded)
		result.Image = req.Image
		result.Window = &w
		if _, current, err := check(); err != nil || scope != current {
			return nil, ErrEngineAccessDenied
		}
	}
	reply, err := s.inference.Segment(ctx, req)
	if err != nil {
		var remote *plugin.RuntimeHTTPError
		if errors.As(err, &remote) && remote.StatusCode == 400 {
			return nil, ErrSegmentImageUnsupported
		}
		return nil, ErrSegmentUpstream
	}
	if _, current, err := check(); err != nil || scope != current {
		return nil, ErrEngineAccessDenied
	}
	result.SessionID = reply.SessionID
	result.Masks = reply.Masks
	return result, nil
}
