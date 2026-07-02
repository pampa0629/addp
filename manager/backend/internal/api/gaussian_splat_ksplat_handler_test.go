package api

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
)

func TestParseKSplatHeader(t *testing.T) {
	headerBytes := make([]byte, ksplatHeaderSizeBytes)
	headerBytes[0] = 0
	headerBytes[1] = 1
	binary.LittleEndian.PutUint32(headerBytes[4:8], 1)
	binary.LittleEndian.PutUint32(headerBytes[8:12], 1)
	binary.LittleEndian.PutUint32(headerBytes[12:16], 256)
	binary.LittleEndian.PutUint32(headerBytes[16:20], 128)
	binary.LittleEndian.PutUint16(headerBytes[20:22], 1)
	putFloat32(headerBytes[24:28], 1.25)
	putFloat32(headerBytes[28:32], -2.5)
	putFloat32(headerBytes[32:36], 3.75)

	header, err := parseKSplatHeader(headerBytes)
	if err != nil {
		t.Fatalf("parseKSplatHeader() error = %v", err)
	}
	if header.VersionMajor != 0 || header.VersionMinor != 1 {
		t.Fatalf("version = %d.%d, want 0.1", header.VersionMajor, header.VersionMinor)
	}
	if header.MaxSectionCount != 1 || header.SectionCount != 1 || header.MaxSplatCount != 256 || header.SplatCount != 128 {
		t.Fatalf("header counts = %#v, want section 1/1 splat 128/256", header)
	}
	if header.CompressionLevel != 1 {
		t.Fatalf("compression = %d, want 1", header.CompressionLevel)
	}
	if len(header.SceneCenter) != 3 || header.SceneCenter[0] != 1.25 || header.SceneCenter[1] != -2.5 || header.SceneCenter[2] != 3.75 {
		t.Fatalf("scene_center = %#v, want [1.25 -2.5 3.75]", header.SceneCenter)
	}
}

func TestInspectGaussianSplatKSplatResultSummarizesHealthyKSplat(t *testing.T) {
	headerBytes := make([]byte, ksplatHeaderSizeBytes)
	headerBytes[0] = 0
	headerBytes[1] = 1
	binary.LittleEndian.PutUint32(headerBytes[4:8], 1)
	binary.LittleEndian.PutUint32(headerBytes[8:12], 1)
	binary.LittleEndian.PutUint32(headerBytes[12:16], 256)
	binary.LittleEndian.PutUint32(headerBytes[16:20], 128)
	binary.LittleEndian.PutUint16(headerBytes[20:22], 1)
	header, err := parseKSplatHeader(headerBytes)
	if err != nil {
		t.Fatalf("parseKSplatHeader() error = %v", err)
	}

	result := &models.GaussianSplatKSplat{
		ID:         7,
		FileName:   "model.ksplat",
		StorageRef: `{"bucket":"manager","object":"model.ksplat"}`,
		SizeBytes:  int64(ksplatHeaderSizeBytes + 1024 + 16),
		Status:     models.GaussianSplatKSplatStatusReady,
	}
	response := inspectGaussianSplatKSplatResult(result, "manager", "model.ksplat", minio.ObjectInfo{
		Size:        int64(ksplatHeaderSizeBytes + 1024 + 16),
		ContentType: ksplatExpectedContentType,
	}, header, ksplatHeaderSizeBytes, "0001", nil)

	if response.Summary.Status != "ok" {
		t.Fatalf("summary = %#v, want ok", response.Summary)
	}
	if response.Header == nil || response.Header.SplatCount != 128 {
		t.Fatalf("header = %#v, want splat_count 128", response.Header)
	}
	for _, check := range response.Checks {
		if check.Status != "ok" {
			t.Fatalf("check %s = %#v, want ok", check.Name, check)
		}
	}
}

func putFloat32(raw []byte, value float32) {
	binary.LittleEndian.PutUint32(raw, math.Float32bits(value))
}
