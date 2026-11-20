package catalogdiscovery

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
)

// Binding is supplied by the deployment administrator. The CA and token are
// mounted into System. Token rotation is observed on every API request.
type Binding struct {
	APIServer  string `json:"api_server"`
	CAFile     string `json:"ca_file"`
	TokenFile  string `json:"token_file"`
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	ServiceUID string `json:"service_uid"`
	PortName   string `json:"port_name"`
}

type Source struct {
	binding     Binding
	client      *http.Client
	trustDigest [sha256.Size]byte
}

func Open(path string) (*Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Binding
	if err = json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	server, err := url.Parse(b.APIServer)
	if err != nil || server.Scheme != "https" || server.Hostname() == "" || server.User != nil || server.RawQuery != "" || server.Fragment != "" || strings.Trim(server.Path, "/") != "" ||
		b.CAFile == "" || b.TokenFile == "" || b.ServiceUID == "" || b.PortName == "" || !validSegment(b.Namespace) || !validSegment(b.Service) {
		return nil, errors.New("invalid Catalog Kubernetes binding")
	}
	pem, err := os.ReadFile(b.CAFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("invalid Kubernetes CA file")
	}
	b.APIServer = strings.TrimRight(b.APIServer, "/")
	return &Source{binding: b, trustDigest: sha256.Sum256(pem), client: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// BindingID scopes a stored observation to the configured cluster trust, Service and port.
func (s *Source) BindingID() string {
	b := s.binding
	key, _ := json.Marshal([]string{fmt.Sprintf("%x", s.trustDigest), b.APIServer, b.Namespace, b.Service, b.ServiceUID, b.PortName})
	return fmt.Sprintf("%x", sha256.Sum256(key))
}

func validSegment(s string) bool {
	return s != "" && s != "." && s != ".." && url.PathEscape(s) == s && !strings.Contains(s, "/")
}

type metadata struct {
	Name, Namespace, UID, ResourceVersion string
	DeletionTimestamp                     *time.Time
	Labels                                map[string]string
	OwnerReferences                       []struct {
		APIVersion, Kind, Name, UID string
		Controller                  bool
	}
}
type service struct {
	Metadata metadata
	Spec     struct {
		Selector                 map[string]string
		PublishNotReadyAddresses bool
		Ports                    []port
	}
}
type port struct {
	Name, Protocol string
	Port           int
}
type slice struct {
	Metadata    metadata
	AddressType string
	Ports       []port
	Endpoints   []struct {
		Addresses  []string
		Conditions struct{ Ready, Terminating bool }
		TargetRef  struct{ Kind, Namespace, UID string }
	}
}
type view struct {
	serviceVersion, sliceVersion string
	slices                       map[string]slice
}
type watchEvent struct {
	Type   string
	Object json.RawMessage
}
type event struct {
	service bool
	watchEvent
}

func (s *Source) paths() (string, string) {
	return "/api/v1/namespaces/" + s.binding.Namespace + "/services", "/apis/discovery.k8s.io/v1/namespaces/" + s.binding.Namespace + "/endpointslices"
}
func (s *Source) open(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	token, err := os.ReadFile(s.binding.TokenFile)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.binding.APIServer+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Kubernetes %s returned %d", path, resp.StatusCode)
	}
	return resp, nil
}
func (s *Source) read(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := s.open(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
func (s *Source) bound(v service) bool {
	b := s.binding
	if v.Metadata.Name != b.Service || v.Metadata.Namespace != b.Namespace || v.Metadata.UID != b.ServiceUID || v.Metadata.DeletionTimestamp != nil || len(v.Spec.Selector) == 0 || v.Spec.PublishNotReadyAddresses {
		return false
	}
	for _, p := range v.Spec.Ports {
		if p.Protocol == "TCP" && p.Name == b.PortName {
			return true
		}
	}
	return false
}
func (s *Source) snapshot(ctx context.Context) (*view, error) {
	sp, ep := s.paths()
	// A field-selected list supplies a collection resourceVersion suitable for watch,
	// including when the previously bound Service has been deleted.
	var services struct {
		Metadata metadata
		Items    []service
	}
	q := url.Values{"fieldSelector": {"metadata.name=" + s.binding.Service}}
	if err := s.read(ctx, sp, q, &services); err != nil {
		return nil, err
	}
	v := &view{serviceVersion: services.Metadata.ResourceVersion, slices: map[string]slice{}}
	if len(services.Items) != 1 || !s.bound(services.Items[0]) {
		return v, nil
	}
	q = url.Values{"labelSelector": {"kubernetes.io/service-name=" + s.binding.Service}, "limit": {"200"}}
	for {
		var page struct {
			Metadata struct{ ResourceVersion, Continue string }
			Items    []slice
		}
		if err := s.read(ctx, ep, q, &page); err != nil {
			return nil, err
		}
		if page.Metadata.ResourceVersion == "" || v.sliceVersion != "" && page.Metadata.ResourceVersion != v.sliceVersion {
			return nil, errors.New("inconsistent EndpointSlice list")
		}
		v.sliceVersion = page.Metadata.ResourceVersion
		for _, item := range page.Items {
			v.slices[item.Metadata.UID] = item
		}
		if page.Metadata.Continue == "" {
			break
		}
		q.Set("continue", page.Metadata.Continue)
	}
	// Confirm that the bound Service still exists after listing its slices.
	var last struct {
		Metadata metadata
		Items    []service
	}
	if err := s.read(ctx, sp, url.Values{"fieldSelector": {"metadata.name=" + s.binding.Service}}, &last); err != nil {
		return nil, err
	}
	if len(last.Items) != 1 || !s.bound(last.Items[0]) || last.Items[0].Metadata.ResourceVersion != services.Items[0].Metadata.ResourceVersion {
		v.slices = nil
	}
	return v, nil
}
func (s *Source) endpoints(v *view, expires time.Time) []models.ModuleRoutingEndpoint {
	b := s.binding
	out := map[string]string{}
	for _, item := range v.slices {
		m := item.Metadata
		if m.DeletionTimestamp != nil || m.Namespace != b.Namespace || m.Labels["kubernetes.io/service-name"] != b.Service || m.Labels["endpointslice.kubernetes.io/managed-by"] != "endpointslice-controller.k8s.io" {
			continue
		}
		owned := false
		for _, o := range m.OwnerReferences {
			if o.Controller && o.APIVersion == "v1" && o.Kind == "Service" && o.Name == b.Service && o.UID == b.ServiceUID {
				owned = true
			}
		}
		if !owned {
			continue
		}
		for _, p := range item.Ports {
			if p.Protocol != "TCP" || p.Name != b.PortName || p.Port < 1 || p.Port > 65535 {
				continue
			}
			for _, e := range item.Endpoints {
				t := e.TargetRef
				if !e.Conditions.Ready || e.Conditions.Terminating || t.Kind != "Pod" || t.Namespace != b.Namespace || t.UID == "" {
					continue
				}
				id := t.UID + "/" + p.Name
				for _, addr := range e.Addresses {
					ip, err := netip.ParseAddr(addr)
					if err != nil || !(item.AddressType == "IPv4" && ip.Is4() || item.AddressType == "IPv6" && ip.Is6()) {
						continue
					}
					target := "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(p.Port))
					if old, ok := out[id]; !ok || target < old {
						out[id] = target
					}
				}
			}
		}
	}
	result := make([]models.ModuleRoutingEndpoint, 0, len(out))
	for id, target := range out {
		result = append(result, models.ModuleRoutingEndpoint{ModuleName: "catalog", EndpointID: id, URL: target, ExpiresAt: expires})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].EndpointID < result[j].EndpointID })
	return result
}

func (s *Source) watch(ctx context.Context, path string, q url.Values, isService bool, events chan<- event) {
	resp, err := s.open(ctx, path, q)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	for {
		var e watchEvent
		if decoder.Decode(&e) != nil || e.Type == "ERROR" {
			return
		}
		select {
		case events <- event{isService, e}:
		case <-ctx.Done():
			return
		}
	}
}

// Run publishes only a complete API view. Watches may subtract previously
// verified endpoints until the next complete read. Only a full read renews time.
func (s *Source) Run(ctx context.Context, publish func([]models.ModuleRoutingEndpoint) error, onError func(error)) {
	for ctx.Err() == nil {
		start := time.Now()
		cycle, cancel := context.WithDeadline(ctx, start.Add(10*time.Second))
		v, err := s.snapshot(cycle)
		if err == nil {
			expires := start.Add(30 * time.Second)
			accepted := s.endpoints(v, expires)
			err = publish(accepted)
			if err == nil && len(accepted) > 0 {
				sp, ep := s.paths()
				events := make(chan event)
				go s.watch(cycle, sp, url.Values{"fieldSelector": {"metadata.name=" + s.binding.Service}, "resourceVersion": {v.serviceVersion}, "watch": {"true"}}, true, events)
				go s.watch(cycle, ep, url.Values{"labelSelector": {"kubernetes.io/service-name=" + s.binding.Service}, "resourceVersion": {v.sliceVersion}, "watch": {"true"}}, false, events)
				for len(accepted) > 0 && err == nil && cycle.Err() == nil {
					select {
					case <-cycle.Done():
					case e := <-events:
						if e.Type != "MODIFIED" && e.Type != "DELETED" && e.Type != "ADDED" {
							continue
						}
						if e.service {
							var svc service
							if json.Unmarshal(e.Object, &svc) != nil || e.Type == "DELETED" || !s.bound(svc) {
								v.slices = nil
							}
						} else {
							var sl slice
							if json.Unmarshal(e.Object, &sl) != nil {
								v.slices = nil
							} else if e.Type == "DELETED" {
								delete(v.slices, sl.Metadata.UID)
							} else {
								v.slices[sl.Metadata.UID] = sl
							}
						}
						current := map[models.ModuleRoutingEndpoint]bool{}
						for _, ep := range s.endpoints(v, expires) {
							current[ep] = true
						}
						kept := make([]models.ModuleRoutingEndpoint, 0, len(accepted))
						for _, ep := range accepted {
							if current[ep] {
								kept = append(kept, ep)
							}
						}
						if len(kept) != len(accepted) {
							accepted = kept
							err = publish(accepted)
						}
					}
				}
			}
		}
		if err != nil && ctx.Err() == nil {
			onError(err)
		}
		<-cycle.Done()
		cancel()
	}
}
