package auditarchive

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
)

// S3Store never provisions buckets, deletes objects or releases legal holds.
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
	v, e := s.Client.PutObject(ctx, s.Bucket, key, bytes.NewReader(data), int64(len(data)), opts)
	return v.VersionID, e
}
func (s *S3Store) Read(ctx context.Context, v Version) ([]byte, Protection, error) {
	if v.ID == "" || v.ID == "null" {
		return nil, Protection{}, ErrUnknown
	}
	mode, until, e := s.Client.GetObjectRetention(ctx, s.Bucket, v.Key, v.ID)
	if e != nil || mode == nil || *mode != minio.Compliance || until == nil {
		return nil, Protection{}, ErrUnknown
	}
	hold, e := s.Client.GetObjectLegalHold(ctx, s.Bucket, v.Key, minio.GetObjectLegalHoldOptions{VersionID: v.ID})
	// A successfully read COMPLIANCE version can have no legal-hold
	// configuration. That explicit absence is distinct from denied/failed reads.
	// Legal Hold is only observed; this runtime cannot set or release it.
	held := false
	if e != nil {
		if minio.ToErrorResponse(e).Code != "NoSuchObjectLockConfiguration" {
			return nil, Protection{}, ErrUnknown
		}
	} else {
		if hold == nil || !hold.IsValid() {
			return nil, Protection{}, ErrUnknown
		}
		held = *hold == minio.LegalHoldEnabled
	}
	o, e := s.Client.GetObject(ctx, s.Bucket, v.Key, minio.GetObjectOptions{VersionID: v.ID})
	if e != nil {
		return nil, Protection{}, ErrUnknown
	}
	defer o.Close()
	b, e := io.ReadAll(io.LimitReader(o, MaxObjectBytes+1))
	if e != nil || len(b) > MaxObjectBytes {
		return nil, Protection{}, ErrUnknown
	}
	return b, Protection{*until, held}, nil
}
func (s *S3Store) Extend(ctx context.Context, v Version, until time.Time) error {
	mode := minio.Compliance
	return s.Client.PutObjectRetention(ctx, s.Bucket, v.Key, minio.PutObjectRetentionOptions{VersionID: v.ID, Mode: &mode, RetainUntilDate: &until})
}
func (s *S3Store) Walk(ctx context.Context, prefix string, visit func(Version) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for o := range s.Client.ListObjects(ctx, s.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if o.Err != nil {
			return ErrUnknown
		}
		if e := visit(Version{o.Key, o.VersionID, o.IsDeleteMarker, o.LastModified}); e != nil {
			return e
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	return nil
}
