package auditarchive

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config is private deployment input. It is never loaded from ADDP's root .env
// and must not be included in reports, command arguments or error messages.
type Config struct {
	Mode          string `json:"mode"`
	SourceID      string `json:"source_id"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	Bucket        string `json:"bucket"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	SessionToken  string `json:"session_token"`
	RetentionDays int    `json:"retention_days"`
	PostgresDSN   string `json:"postgres_dsn"`
	Slot          string `json:"slot"`
	Publication   string `json:"publication"`
	Actor         string `json:"actor"`
}

func ReadConfig(path string) (Config, error) {
	var c Config
	stat, e := os.Lstat(path)
	if e != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return c, ErrUnknown
	}
	f, e := os.Open(path)
	if e != nil {
		return c, ErrUnknown
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(stat, actual) {
		return c, ErrUnknown
	}
	data, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(data) > 65536 {
		return c, ErrBudget
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF {
		return Config{}, ErrUnknown
	}
	if c.Validate() != nil {
		return Config{}, ErrUnknown
	}
	return c, nil
}

func (c Config) Validate() error {
	if _, e := sourcePrefix(c.SourceID); e != nil {
		return e
	}
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || c.Region == "" || c.AccessKey == "" || c.SecretKey == "" || c.Bucket == "" || c.RetentionDays < 1 || c.RetentionDays > 36500 {
		return ErrUnknown
	}
	switch c.Mode {
	case "collector":
		urlConfig, urlErr := url.Parse(c.PostgresDSN)
		if urlErr != nil || (urlConfig.Scheme != "postgres" && urlConfig.Scheme != "postgresql") || urlConfig.Host == "" || urlConfig.User == nil || urlConfig.User.Username() == "" || urlConfig.Path == "" || urlConfig.Path == "/" || urlConfig.Query().Get("sslmode") != "verify-full" || urlConfig.Query().Get("sslrootcert") == "" {
			return ErrUnknown
		}
		password, passwordSet := urlConfig.User.Password()
		if (!passwordSet || password == "") && (urlConfig.Query().Get("sslcert") == "" || urlConfig.Query().Get("sslkey") == "") {
			return ErrUnknown
		}
		pg, e := pgconn.ParseConfig(c.PostgresDSN)
		if e != nil || c.PostgresDSN == "" || pg.TLSConfig == nil || pg.TLSConfig.InsecureSkipVerify || pg.TLSConfig.ServerName == "" || len(pg.Fallbacks) != 0 || !identifier(c.Slot) || !identifier(c.Publication) || c.Actor != "" {
			return ErrUnknown
		}
	case "auditor":
		u, e := uuid.Parse(c.Actor)
		if e != nil || u.String() != c.Actor || c.PostgresDSN != "" || c.Slot != "" || c.Publication != "" {
			return ErrUnknown
		}
	default:
		return ErrUnknown
	}
	return nil
}

func (c Config) Store() (*S3Store, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	u, _ := url.Parse(c.Endpoint)
	client, e := minio.New(u.Host, &minio.Options{Secure: true, Region: c.Region, MaxRetries: 1, Creds: credentials.NewStaticV4(c.AccessKey, c.SecretKey, c.SessionToken)})
	if e != nil {
		return nil, ErrUnknown
	}
	return &S3Store{Client: client, Bucket: c.Bucket}, nil
}

func (c Config) PostgreSQL() PostgreSQL {
	return PostgreSQL{DSN: c.PostgresDSN, SourceID: c.SourceID, Slot: c.Slot, Publication: c.Publication, RetentionDays: c.RetentionDays}
}
