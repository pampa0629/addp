package catalogdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

const svcJSON = `{"metadata":{"name":"catalog","namespace":"pilot","uid":"bound","resourceVersion":"5"},"spec":{"selector":{"app":"catalog"},"ports":[{"name":"http","protocol":"TCP","port":80}]}}`
const sliceJSON = `{"metadata":{"namespace":"pilot","uid":"slice1","labels":{"kubernetes.io/service-name":"catalog","endpointslice.kubernetes.io/managed-by":"endpointslice-controller.k8s.io"},"ownerReferences":[{"apiVersion":"v1","kind":"Service","name":"catalog","uid":"bound","controller":true}]},"addressType":"IPv4","ports":[{"name":"http","protocol":"TCP","port":8192}],"endpoints":[{"addresses":["10.42.1.7"],"conditions":{"ready":true},"targetRef":{"kind":"Pod","namespace":"pilot","uid":"pod1"}}]}`

func TestEndpointEligibility(t *testing.T) {
	s := Source{binding: Binding{Namespace: "pilot", Service: "catalog", ServiceUID: "bound", PortName: "http"}}
	for _, tc := range []struct{ from, to string }{
		{"bound", "another"}, {"endpointslice-controller.k8s.io", "foreign"}, {"\"controller\":true", "\"controller\":false"}, {"\"ready\":true", "\"ready\":null"}, {"\"ready\":true", "\"ready\":true,\"terminating\":true"}, {"\"kind\":\"Pod\"", "\"kind\":\"Node\""}, {"TCP", "UDP"}, {"IPv4", "IPv6"},
	} {
		var sl slice
		if err := json.Unmarshal([]byte(strings.Replace(sliceJSON, tc.from, tc.to, 1)), &sl); err != nil {
			t.Fatal(err)
		}
		if got := s.endpoints(&view{slices: map[string]slice{"slice1": sl}}, time.Now()); len(got) != 0 {
			t.Errorf("%s -> %s admitted %+v", tc.from, tc.to, got)
		}
	}
}

func TestReconcileRequiresCompleteListAndWatchOnlyRemoves(t *testing.T) {
	for _, fault := range []string{"", "replace service", "second page failed"} {
		t.Run("fault="+fault, func(t *testing.T) {
			token := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(token, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}
			var reads int // service list requests are sequential
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("wrong credential")
					w.WriteHeader(403)
					return
				}
				q := r.URL.Query()
				if strings.HasSuffix(r.URL.Path, "/services") {
					if q.Get("fieldSelector") != "metadata.name=catalog" {
						t.Error("unscoped service")
					}
					if q.Get("watch") == "true" {
						<-r.Context().Done()
						return
					}
					reads++
					obj := svcJSON
					if reads == 2 && fault == "replace service" {
						obj = strings.Replace(obj, "bound", "new", 1)
					}
					fmt.Fprintf(w, `{"metadata":{"resourceVersion":"6"},"items":[%s]}`, obj)
					return
				}
				if q.Get("labelSelector") != "kubernetes.io/service-name=catalog" {
					t.Error("unscoped endpoints")
				}
				if q.Get("watch") == "true" {
					if q.Get("resourceVersion") != "8" {
						t.Error("wrong watch cursor")
					}
					fmt.Fprintf(w, `{"type":"ADDED","object":%s}`+"\n", strings.ReplaceAll(strings.ReplaceAll(sliceJSON, "slice1", "slice2"), "pod1", "pod2"))
					fmt.Fprintf(w, `{"type":"DELETED","object":%s}`+"\n", sliceJSON)
					return
				}
				if q.Get("continue") == "" {
					fmt.Fprintf(w, `{"metadata":{"resourceVersion":"8","continue":"next"},"items":[%s]}`, sliceJSON)
					return
				}
				if fault == "second page failed" {
					w.WriteHeader(503)
					return
				}
				fmt.Fprint(w, `{"metadata":{"resourceVersion":"8"},"items":[]}`)
			}))
			defer srv.Close()
			s := Source{binding: Binding{APIServer: srv.URL, TokenFile: token, Namespace: "pilot", Service: "catalog", ServiceUID: "bound", PortName: "http"}, client: srv.Client()}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var published [][]models.ModuleRoutingEndpoint
			var readErr error
			before := time.Now()
			s.Run(ctx, func(ep []models.ModuleRoutingEndpoint) error {
				published = append(published, ep)
				if len(ep) == 0 {
					cancel()
				}
				return nil
			}, func(err error) { readErr = err; cancel() })
			switch fault {
			case "replace service":
				if len(published) != 1 || len(published[0]) != 0 {
					t.Fatalf("replacement admitted %+v", published)
				}
			case "second page failed":
				if readErr == nil || len(published) != 0 {
					t.Fatalf("partial read published %+v, error %v", published, readErr)
				}
			default:
				if len(published) != 2 || len(published[0]) != 1 || len(published[1]) != 0 {
					t.Fatalf("watch publications %+v", published)
				}
				first := published[0][0]
				if first.EndpointID != "pod1/http" || first.URL != "http://10.42.1.7:8192" || first.ExpiresAt.Before(before.Add(30*time.Second)) || first.ExpiresAt.After(time.Now().Add(30*time.Second)) {
					t.Errorf("endpoint %+v", first)
				}
			}
		})
	}
}
