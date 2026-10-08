package resourcequery

import (
	"encoding/json"
	"fmt"
	"testing"
)

func collectionWire(t *testing.T, fields map[string]string) envelope {
	t.Helper()
	data := envelope{Status: "success"}
	data.Data.ResultType = "vector"
	data.Data.Result = []wireSeries{}
	for k, v := range fields {
		var pair sample
		if err := json.Unmarshal([]byte(fmt.Sprintf(`[%d,%q]`, testTime.Unix(), v)), &pair); err != nil {
			t.Fatal(err)
		}
		data.Data.Result = append(data.Data.Result, wireSeries{Metric: map[string]string{"signal": k}, Value: &pair})
	}
	return data
}
func TestCollectionEvidenceSeparatesConnectivityAndCollectorCoverage(t *testing.T) {
	stamp := fmt.Sprint(testTime.Unix())
	cases := []struct {
		name              string
		fields            map[string]string
		state, filesystem string
	}{
		{"not discovered", map[string]string{"up_count": "0", "filesystem_count": "0"}, "no_sample", "unknown"},
		{"successful source without filesystem collector", map[string]string{"up_count": "1", "up": "1", "sampled_at": stamp, "filesystem_count": "0"}, "collecting", "not_collected"},
		{"source scrape failed with old collector evidence", map[string]string{"up_count": "1", "up": "0", "sampled_at": stamp, "filesystem_count": "1", "filesystem": "1", "filesystem_sampled_at": stamp}, "failed", "unknown"},
		{"expired up", map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(testTime.Unix() - 61), "filesystem_count": "0"}, "stale", "unknown"},
		{"duplicate target", map[string]string{"up_count": "2", "up": "1", "sampled_at": stamp, "filesystem_count": "0"}, "no_sample", "unknown"},
		{"filesystem ready", map[string]string{"up_count": "1", "up": "1", "sampled_at": stamp, "filesystem_count": "1", "filesystem": "1", "filesystem_sampled_at": stamp}, "collecting", "available"},
		{"filesystem failed", map[string]string{"up_count": "1", "up": "1", "sampled_at": stamp, "filesystem_count": "1", "filesystem": "0", "filesystem_sampled_at": stamp}, "collecting", "failed"},
		{"different filesystem scrape", map[string]string{"up_count": "1", "up": "1", "sampled_at": stamp, "filesystem_count": "1", "filesystem": "1", "filesystem_sampled_at": fmt.Sprint(testTime.Unix() - 15)}, "collecting", "unknown"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCollection(collectionWire(t, tt.fields), testTime)
			if err != nil || got.State != tt.state || got.Filesystem != tt.filesystem {
				t.Fatalf("got=%+v error=%v", got, err)
			}
		})
	}
}
func TestCollectionRejectsInvalidAndUnboundedEvidence(t *testing.T) {
	valid := map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(testTime.Unix()), "filesystem_count": "0"}
	for _, tt := range []struct{ key, value string }{{"up", "2"}, {"up", "NaN"}, {"sampled_at", fmt.Sprint(testTime.Unix() + 1)}, {"up_count", "0.5"}, {"filesystem_count", "-1"}, {"unknown", "1"}} {
		t.Run(tt.key+tt.value, func(t *testing.T) {
			fields := map[string]string{}
			for k, v := range valid {
				fields[k] = v
			}
			fields[tt.key] = tt.value
			if _, err := normalizeCollection(collectionWire(t, fields), testTime); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	data := collectionWire(t, valid)
	data.Data.Result[0].Metric["endpoint"] = "private.test"
	if _, err := normalizeCollection(data, testTime); err == nil {
		t.Fatal("arbitrary label accepted")
	}
	data = collectionWire(t, valid)
	data.Data.Result = append(data.Data.Result, data.Data.Result[0])
	if _, err := normalizeCollection(data, testTime); err == nil {
		t.Fatal("duplicate signal accepted")
	}
}
