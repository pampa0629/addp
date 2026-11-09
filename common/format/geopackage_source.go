package format

import (
	"context"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/addp/common/engine/plugin"
)

// GeoPackageSourceObservation is a point-in-time NFS observation of one
// standalone-file candidate. It does not authorize reads or identify a snapshot.
type GeoPackageSourceObservation struct {
	SizeBytes int64
	ETag      string
	UpdatedAt time.Time
}

func (o GeoPackageSourceObservation) Matches(other GeoPackageSourceObservation) bool {
	return o.SizeBytes == other.SizeBytes && o.ETag == other.ETag && o.UpdatedAt.Equal(other.UpdatedAt)
}

// ObserveGeoPackageSource checks exactly the primary and the three SQLite
// sidecar names. A nil observation means standalone storage could not be proved.
func ObserveGeoPackageSource(ctx context.Context, p plugin.EngineCatalogFactsProvider, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*GeoPackageSourceObservation, error) {
	if p == nil || p.Type() != "nfs" {
		return nil, nil
	}
	path, err := plugin.RequireFileLeafPath(target)
	if err != nil || !reflect.DeepEqual(target, plugin.FileItemPath(target.EngineID, path)) {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, conn, target, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	if facts == nil || !reflect.DeepEqual(facts.Path, target) || facts.Kind != "file" || facts.Storage == nil ||
		facts.Storage.Path != path || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes < 0 {
		return nil, nil
	}
	observation := &GeoPackageSourceObservation{SizeBytes: *facts.Storage.SizeBytes, ETag: facts.Storage.ETag}
	if facts.UpdatedAt != nil {
		observation.UpdatedAt = *facts.UpdatedAt
	}
	if observation.ETag == "" && observation.UpdatedAt.IsZero() {
		return nil, nil
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err := p.DescribeEngineCatalogFacts(ctx, conn, plugin.FileItemPath(target.EngineID, path+suffix), plugin.EngineCatalogFactsOptions{})
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return observation, nil
}
