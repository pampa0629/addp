package service

import (
	"errors"
	"github.com/addp/common/opaquetoken"
	"github.com/addp/service/internal/models"
)

const queryTokenVersion = 1

var (
	ErrInvalidQueryCursor = errors.New("invalid query cursor")
	ErrInvalidFeatureID   = errors.New("invalid feature id")
)

type queryCursorPayload struct {
	Version        int                 `json:"v"`
	ServiceID      uint                `json:"service_id"`
	ServiceVersion string              `json:"service_version"`
	QueryHash      string              `json:"query_hash"`
	OrderBy        []models.QueryOrder `json:"order_by"`
	Values         []interface{}       `json:"values"`
}

type featureIDPayload struct {
	Version        int           `json:"v"`
	ServiceID      uint          `json:"service_id"`
	ServiceVersion string        `json:"service_version"`
	Fields         []string      `json:"fields"`
	Values         []interface{} `json:"values"`
}

type queryTokenCodec struct{ codec *opaquetoken.Codec }

func newQueryTokenCodec(encryptionKey []byte) *queryTokenCodec {
	return &queryTokenCodec{codec: opaquetoken.New(encryptionKey, "addp/service/query-token/v1")}
}

func (c *queryTokenCodec) encodeCursor(payload queryCursorPayload) (string, error) {
	payload.Version = queryTokenVersion
	return c.codec.Encode("cursor", payload)
}

func (c *queryTokenCodec) decodeCursor(token string) (*queryCursorPayload, error) {
	var payload queryCursorPayload
	if err := c.codec.Decode("cursor", token, &payload); err != nil || payload.Version != queryTokenVersion {
		return nil, ErrInvalidQueryCursor
	}
	return &payload, nil
}

func (c *queryTokenCodec) encodeFeatureID(payload featureIDPayload) (string, error) {
	payload.Version = queryTokenVersion
	return c.codec.Encode("feature", payload)
}

func (c *queryTokenCodec) decodeFeatureID(token string) (*featureIDPayload, error) {
	var payload featureIDPayload
	if err := c.codec.Decode("feature", token, &payload); err != nil || payload.Version != queryTokenVersion {
		return nil, ErrInvalidFeatureID
	}
	return &payload, nil
}
