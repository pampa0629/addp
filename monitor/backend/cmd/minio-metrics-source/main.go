// minio-metrics-source is a deployment-only fixed collector, not Monitor Web.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"time"

	"github.com/addp/common/jsonmap"
	"github.com/addp/monitor/internal/miniosource"
	"github.com/addp/monitor/internal/sourceserver"
)

func decode(reader io.Reader) (miniosource.Target, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 32769))
	if err != nil {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	fields, err := jsonmap.DecodeObjectStrict(data, 32768, 2)
	if err != nil || len(fields) != 6 {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	for _, key := range []string{"address", "scope", "access_key", "secret_key", "container_id", "container_started_at"} {
		if _, ok := fields[key].(string); !ok {
			return miniosource.Target{}, miniosource.ErrUnavailable
		}
	}
	var config struct {
		Address   string `json:"address"`
		Scope     string `json:"scope"`
		Access    string `json:"access_key"`
		Secret    string `json:"secret_key"`
		Container string `json:"container_id"`
		Started   string `json:"container_started_at"`
	}
	if json.Unmarshal(data, &config) != nil {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	address, err := netip.ParseAddrPort(config.Address)
	if err != nil {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	started, err := time.Parse(time.RFC3339Nano, config.Started)
	if err != nil {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	target := miniosource.Target{Address: address, Scope: config.Scope, AccessKey: config.Access, SecretKey: config.Secret, ContainerID: config.Container, ContainerStartedAt: started}
	if !target.Valid() {
		return miniosource.Target{}, miniosource.ErrUnavailable
	}
	return target, nil
}

func run(args []string) error {
	if len(args) != 0 {
		return errors.New("unexpected source arguments")
	}
	file, err := os.Open("/source/config.json")
	if err != nil {
		return miniosource.ErrUnavailable
	}
	defer file.Close()
	target, err := decode(file)
	if err != nil {
		return err
	}
	return sourceserver.Serve(miniosource.Handler(func(ctx context.Context) (miniosource.Snapshot, error) { return miniosource.Read(ctx, target) }), "/source")
}

func main() {
	if run(os.Args[1:]) != nil {
		_, _ = io.WriteString(os.Stderr, "MinIO observation source unavailable\n")
		os.Exit(1)
	}
}
