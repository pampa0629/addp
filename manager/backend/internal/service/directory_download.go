package service

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

var ErrDownloadSourceChanged = errors.New("download source changed")

// A directory is a format-defined whole DataItem, never a general node grant.
// Planning only consumes committed Meta identity; source enumeration happens
// later, under the current User's whole-item source checks.
func directoryDownloadPlan(engineType string, engineID uint, name string, descriptor dataitem.ItemDescriptor) (*models.DownloadPlan, error) {
	root := plugin.FileDirectoryPath(engineID, name)
	if engineType != "nfs" || descriptor.Format != "filegdb" || descriptor.DataType != datatype.Container ||
		descriptor.Layout != format.LayoutWhole || len(descriptor.Refs) != 0 ||
		!authorizationfacts.ValidWholeScopeRoot(root) || root.StringPath() != name || !validDownloadBundlePath(name) {
		return nil, ErrDownloadNotSupported
	}
	p, err := plugin.Get(engineType)
	if err != nil {
		return nil, ErrDownloadNotSupported
	}
	if _, ok := p.(plugin.EngineCatalogProvider); !ok {
		return nil, ErrDownloadNotSupported
	}
	if _, ok := p.(plugin.ContentReadableProvider); !ok {
		return nil, ErrDownloadNotSupported
	}
	return &models.DownloadPlan{Kind: models.DownloadKindBundle, FileName: path.Base(name) + ".zip", ContentType: "application/zip", DirectoryRoot: &root}, nil
}

type directoryDownloadMember struct {
	ref       string
	directory bool
	size      int64
	modified  time.Time
}

type preparedDirectoryArchive struct {
	*sourceContextReader
	file *os.File
	size int64
}

func (r *preparedDirectoryArchive) Close() error { return r.file.Close() }
func (r *preparedDirectoryArchive) Size() int64  { return r.size }

func (s *MetadataService) openDirectoryDownload(ctx context.Context, read *storageRead, plan *models.DownloadPlan) (_ io.ReadCloser, resultErr error) {
	if read.wholeRoot == nil || plan.Kind != models.DownloadKindBundle || len(plan.Refs) != 0 || !reflect.DeepEqual(read.wholeRoot, plan.DirectoryRoot) {
		return nil, ErrInvalidDownloadBundle
	}
	before, err := read.directoryMembers(ctx)
	if err != nil {
		return nil, err
	}
	// An unnamed request-private file is reclaimed on Close or process exit.
	// No persistent artifact, background job or source copy is created.
	file, err := os.CreateTemp("", "addp-manager-filegdb-download-*.zip")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		return nil, errors.Join(err, file.Close(), os.Remove(file.Name()))
	}
	returned := false
	defer func() {
		if !returned {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	archive := zip.NewWriter(file)
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, archive.Close())
		}
	}()
	root := read.wholeRoot.StringPath()
	prefix := strings.TrimSuffix(root, path.Base(root))
	if _, err := archive.CreateHeader(&zip.FileHeader{Name: path.Base(root) + "/", Method: zip.Store}); err != nil {
		return nil, err
	}
	for _, member := range before {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(member.ref, prefix)
		if member.directory {
			if _, err := archive.CreateHeader(&zip.FileHeader{Name: name + "/", Method: zip.Store}); err != nil {
				return nil, err
			}
			continue
		}
		stream, length, _, _, err := read.stream(ctx, member.ref, "")
		if err != nil {
			return nil, err
		}
		if length != member.size {
			return nil, errors.Join(ErrDownloadSourceChanged, stream.Close())
		}
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: member.modified})
		if err != nil {
			return nil, errors.Join(err, stream.Close())
		}
		copied, copyErr := io.Copy(entry, &sourceContextReader{ctx: ctx, reader: stream})
		closeErr := stream.Close()
		if copyErr != nil || closeErr != nil {
			return nil, errors.Join(copyErr, closeErr)
		}
		if copied != member.size {
			return nil, ErrDownloadSourceChanged
		}
	}
	after, err := read.directoryMembers(ctx)
	if err != nil {
		return nil, err
	}
	if !sameDirectoryDownloadMembers(before, after) {
		return nil, ErrDownloadSourceChanged
	}
	closed = true
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := read.checkWhole(ctx, root); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	returned = true
	return &preparedDirectoryArchive{sourceContextReader: &sourceContextReader{ctx: ctx, reader: file}, file: file, size: info.Size()}, nil
}

func (r *storageRead) directoryMembers(ctx context.Context) ([]directoryDownloadMember, error) {
	p, err := plugin.Get(r.target.EngineType)
	if err != nil {
		return nil, err
	}
	catalog, ok := p.(plugin.EngineCatalogProvider)
	if !ok {
		return nil, ErrDownloadNotSupported
	}
	queue := []plugin.EngineCatalogPath{*r.wholeRoot}
	seen := map[string]bool{}
	members := make([]directoryDownloadMember, 0)
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := r.checkWhole(ctx, parent.StringPath()); err != nil {
			return nil, err
		}
		entries, err := catalog.ListChildren(ctx, plugin.ConnectionInfo(r.target.engine.ConnectionInfo), parent, plugin.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !validDownloadBundlePath(entry.Name) || strings.Contains(entry.Name, "/") {
				return nil, ErrInvalidDownloadBundle
			}
			ref := parent.StringPath() + "/" + entry.Name
			if seen[ref] {
				return nil, ErrInvalidDownloadBundle
			}
			seen[ref] = true
			member := directoryDownloadMember{ref: ref}
			expected := plugin.FileItemPath(r.target.EngineID, ref)
			switch {
			case entry.Term == plugin.EngineCatalogTermDirectory && entry.Kind == plugin.EngineCatalogKindDirectory && entry.Role == plugin.EngineCatalogRoleBranch:
				member.directory = true
				expected = plugin.FileDirectoryPath(r.target.EngineID, ref)
			case entry.Term == plugin.EngineCatalogTermFile && entry.Kind == plugin.EngineCatalogKindFile && entry.Role == plugin.EngineCatalogRoleLeaf:
				if entry.Storage == nil || entry.Storage.SizeBytes == nil || *entry.Storage.SizeBytes < 0 || entry.UpdatedAt == nil {
					return nil, ErrInvalidDownloadBundle
				}
				member.size = *entry.Storage.SizeBytes
				member.modified = entry.UpdatedAt.UTC()
			default:
				return nil, ErrInvalidDownloadBundle
			}
			if !reflect.DeepEqual(entry.Path, expected) {
				return nil, ErrInvalidDownloadBundle
			}
			members = append(members, member)
			if member.directory {
				queue = append(queue, entry.Path)
			}
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ref < members[j].ref })
	return members, nil
}

func sameDirectoryDownloadMembers(a, b []directoryDownloadMember) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ref != b[i].ref || a[i].directory != b[i].directory || a[i].size != b[i].size || !a[i].modified.Equal(b[i].modified) {
			return false
		}
	}
	return true
}
