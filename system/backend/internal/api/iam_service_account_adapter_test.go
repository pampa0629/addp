package api

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestParseServiceAccountOwnerScope(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      string
		wantEmpty bool
		wantError bool
	}{
		{name: "empty", raw: "", wantEmpty: true},
		{name: "tenant", raw: " tenant ", want: "tenant"},
		{name: "platform", raw: "platform", want: "platform"},
		{name: "unsupported", raw: "user", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseServiceAccountOwnerScope(test.raw)
			if test.wantError {
				if !errors.Is(err, commonapi.ErrBadRequest) {
					t.Fatalf("error = %v, want bad request", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse owner scope: %v", err)
			}
			if test.wantEmpty {
				if got != nil {
					t.Fatalf("owner scope = %q, want nil", *got)
				}
				return
			}
			if got == nil || *got != test.want {
				t.Fatalf("owner scope = %v, want %q", got, test.want)
			}
		})
	}
}
