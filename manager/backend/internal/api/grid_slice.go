package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/gin-gonic/gin"
)

func gridSliceFromQuery(c *gin.Context) (*format.GridSliceSelection, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, format.ErrGridSliceSelection
	}
	values, present := query["grid_slice"]
	if !present {
		return nil, nil
	}
	for k, v := range query {
		if (k != "locator" && k != "grid_slice") || len(v) != 1 {
			return nil, format.ErrGridSliceSelection
		}
	}
	if len(values) != 1 || len(values[0]) > 4096 {
		return nil, format.ErrGridSliceSelection
	}
	fields, strictErr := commonJSON.DecodeObjectStrict([]byte(values[0]), 4096, 4)
	if strictErr != nil || len(fields) != 6 {
		return nil, format.ErrGridSliceSelection
	}

	for _, key := range []string{"x", "y", "width", "height"} {
		if _, ok := fields[key].(json.Number); !ok {
			return nil, format.ErrGridSliceSelection
		}
	}
	decoder := json.NewDecoder(strings.NewReader(values[0]))
	decoder.DisallowUnknownFields()
	var selection format.GridSliceSelection
	if decoder.Decode(&selection) != nil || selection.Indices == nil || selection.Validate() != nil {
		return nil, format.ErrGridSliceSelection
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, format.ErrGridSliceSelection
	}
	return &selection, nil
}
func writeGridSliceError(c *gin.Context, err error) bool {
	if !errors.Is(err, format.ErrGridSliceSelection) {
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, manageri18n.MsgGridSliceInvalid), "error_code": "grid_slice_selection_invalid"})
	return true
}
