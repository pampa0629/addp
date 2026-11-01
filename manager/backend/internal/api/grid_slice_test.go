package api

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGridSlicePublicQueryRequiresExactBoundedSelection(t *testing.T) {
	valid := `{"variable":"/temperature","indices":[1],"x":0,"y":0,"width":64,"height":64}`
	for _, test := range []struct {
		raw string
		ok  bool
	}{
		{"locator=addp://engine/1/path/grid.nc&grid_slice=" + url.QueryEscape(valid), true},
		{"locator=x&grid_slice=" + url.QueryEscape(valid) + "&grid_slice=" + url.QueryEscape(valid), false},
		{"locator=x&grid_slice=" + url.QueryEscape(valid) + "&child_name=other", false},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","indices":[],"x":0,"y":0,"width":128,"height":128}`), false},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","indices":[9007199254740993],"x":0,"y":0,"width":1,"height":1}`), true},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","x":0,"y":0,"width":1,"height":1,"other":0}`), false},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","variable":"/t","indices":[],"x":0,"y":0,"width":1,"height":1}`), false},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","indices":[],"x":null,"y":0,"width":1,"height":1}`), false},
		{"locator=x&grid_slice=" + url.QueryEscape(`{"variable":"/t","indices":null,"x":0,"y":0,"width":1,"height":1}`), false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/preview?"+test.raw, nil)
		selection, err := gridSliceFromQuery(c)
		if (err == nil && selection != nil) != test.ok {
			t.Fatal(test.raw, selection, err)
		}
	}
}

func TestGridSliceCannotReadRawValuesWithPreviewPermissionOnly(t *testing.T) {
	h := &ExplorerHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/preview?locator=x&grid_slice="+url.QueryEscape(`{"variable":"/t","indices":[],"x":0,"y":0,"width":1,"height":1}`), nil)
	// No raw-content role permission: reject before resolver, Meta or Runtime IO.
	h.Preview(c)
	if w.Code != 403 {
		t.Fatalf("raw read without content permission: %d", w.Code)
	}
}
