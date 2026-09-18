package falkor

import "testing"

func TestTimeoutConfigurationContract(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{[]any{"TIMEOUT_MAX", int64(2000)}, true},
		{[]any{"TIMEOUT_MAX", int64(1)}, true},
		{[]any{"TIMEOUT_MAX", int64(0)}, false},
		{[]any{"TIMEOUT_MAX", int64(2001)}, false},
		{[]any{"TIMEOUT_DEFAULT", int64(2000)}, false},
		{[]any{"TIMEOUT_MAX", "2000"}, false},
		{[]any{[]any{"TIMEOUT_MAX", int64(2000)}}, false},
		{nil, false},
	} {
		if got := validTimeoutConfig(tc.value, "TIMEOUT_MAX"); got != tc.want {
			t.Fatalf("%v: %v", tc.value, got)
		}
	}
}
