package auditarchive

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
)

// S3Store never provisions buckets or changes retention policies. Those belong
// to the independently controlled deployment, not to the collector process.
type S3Store struct {
	Client *minio.Client
	Bucket string
}

func (s *S3Store) Create(ctx context.Context, key string, data []byte, until time.Time) (string, error) {
	if len(data) > MaxObjectBytes {
		return "", ErrBudget
	}
	opts := minio.PutObjectOptions{ContentType: "application/json", Mode: minio.Compliance, RetainUntilDate: until, DisableMultipart: true}
	opts.SetMatchETagExcept("*")
	v, err := s.Client.PutObject(ctx, s.Bucket, key, bytes.NewReader(data), int64(len(data)), opts)
	return v.VersionID, err
}

func (s *S3Store) Read(ctx context.Context, v Version) ([]byte, time.Time, error) {
	if v.ID == "" || v.ID == "null" {
		return nil, time.Time{}, ErrUnknown
	}
	mode, until, err := s.Client.GetObjectRetention(ctx, s.Bucket, v.Key, v.ID)
	if err != nil || mode == nil || *mode != minio.Compliance || until == nil {
		return nil, time.Time{}, ErrUnknown
	}
	o, err := s.Client.GetObject(ctx, s.Bucket, v.Key, minio.GetObjectOptions{VersionID: v.ID})
	if err != nil {
		return nil, time.Time{}, ErrUnknown
	}
	defer o.Close()
	data, err := io.ReadAll(io.LimitReader(o, MaxObjectBytes+1))
	if err != nil || len(data) > MaxObjectBytes {
		return nil, time.Time{}, ErrUnknown
	}
	return data, *until, nil
}

func (s *S3Store) Versions(ctx context.Context, prefix string) ([]Version, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var versions []Version
	for o := range s.Client.ListObjects(ctx, s.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if o.Err != nil {
			return nil, ErrUnknown
		}
		versions = append(versions, Version{Key: o.Key, ID: o.VersionID, DeleteMarker: o.IsDeleteMarker})
		if len(versions) > MaxChainObjects {
			return nil, ErrBudget
		}
	}
	return versions, nil
}
