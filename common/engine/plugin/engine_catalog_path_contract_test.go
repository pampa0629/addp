package plugin

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
)

func TestCatalogProviderHelpersRejectEmptyParentPath(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "object",
			run: func() error {
				_, err := ListObjectCatalogChildren(ctx, ObjectCatalogCallbacks{
					ListBucketsFunc: func(context.Context, ConnectionInfo, EngineCatalogPath) ([]EngineCatalogEntry, error) {
						return nil, nil
					},
				}, nil, 1, EngineCatalogPath{}, ListOptions{})
				return err
			},
		},
		{
			name: "file",
			run: func() error {
				_, err := ListFileCatalogChildren(ctx, FileCatalogCallbacks{
					ListDirectoryFunc: func(context.Context, ConnectionInfo, EngineCatalogPath) ([]EngineCatalogEntry, error) {
						return nil, nil
					},
				}, nil, 1, EngineCatalogPath{}, ListOptions{})
				return err
			},
		},
		{
			name: "tabular",
			run: func() error {
				_, err := ListTabularCatalogChildren(ctx, minimalTabularCatalogCallbacks(), &Engine{ID: 1, EngineType: "unregistered"}, EngineCatalogPath{}, ListOptions{})
				return err
			},
		},
		{
			name: "dynamic-schema",
			run: func() error {
				_, err := ListDynamicSchemaCatalogChildren(ctx, DynamicSchemaCatalogCallbacks{
					ListNamespacesFunc: func(context.Context, ConnectionInfo, EngineCatalogPath) ([]EngineCatalogEntry, error) {
						return nil, nil
					},
				}, 1, nil, EngineCatalogPath{}, ListOptions{})
				return err
			},
		},
		{
			name: "graph",
			run: func() error {
				_, err := ListGraphCatalogChildren(ctx, GraphCatalogCallbacks{
					ListNamespacesFunc: func(context.Context, ConnectionInfo, EngineCatalogPath) ([]EngineCatalogEntry, error) {
						return nil, nil
					},
				}, 1, nil, EngineCatalogPath{}, ListOptions{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("helper accepted empty parent path")
			}
		})
	}
}

func TestCatalogFactsHelpersRejectEmptyAndRootPath(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		root EngineCatalogPath
		run  func(EngineCatalogPath) error
	}{
		{
			name: "object",
			root: ObjectRootPath(1),
			run: func(path EngineCatalogPath) error {
				_, err := DescribeObjectCatalogFacts(ctx, ObjectCatalogCallbacks{}, nil, 1, path)
				return err
			},
		},
		{
			name: "file",
			root: FileRootPath(1),
			run: func(path EngineCatalogPath) error {
				_, err := DescribeFileCatalogFacts(ctx, FileCatalogCallbacks{}, nil, 1, path)
				return err
			},
		},
		{
			name: "dynamic-schema",
			root: EngineCatalogRootPath(DynamicSchemaCatalogModel(), 1),
			run: func(path EngineCatalogPath) error {
				_, err := DescribeDynamicSchemaCatalogFacts(ctx, DynamicSchemaCatalogCallbacks{}, 1, nil, path, EngineCatalogFactsOptions{})
				return err
			},
		},
		{
			name: "graph",
			root: EngineCatalogRootPath(GraphCatalogModel(), 1),
			run: func(path EngineCatalogPath) error {
				_, err := DescribeGraphCatalogFacts(ctx, GraphCatalogCallbacks{}, 1, nil, path, EngineCatalogFactsOptions{})
				return err
			},
		},
		{
			name: "tabular",
			root: EngineCatalogRootPath(TabularCatalogModel(EngineCatalogTermDatabase), 1),
			run: func(path EngineCatalogPath) error {
				_, err := DescribeTabularCatalogFacts(ctx, minimalTabularCatalogCallbacks(), &Engine{ID: 1, EngineType: "unregistered"}, path, EngineCatalogFactsOptions{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/empty", func(t *testing.T) {
			if err := tt.run(EngineCatalogPath{}); err == nil {
				t.Fatal("helper accepted empty facts path")
			}
		})
		t.Run(tt.name+"/root", func(t *testing.T) {
			if err := tt.run(tt.root); err == nil {
				t.Fatal("helper accepted root facts path")
			}
		})
	}
}

func TestContentPathHelpersRequireLeafPath(t *testing.T) {
	tests := []struct {
		name    string
		root    EngineCatalogPath
		branch  EngineCatalogPath
		leaf    EngineCatalogPath
		run     func(EngineCatalogPath) (string, error)
		want    string
		wantErr string
	}{
		{
			name:   "object",
			root:   ObjectRootPath(1),
			branch: ObjectDirectoryPath(1, "bucket", "prefix"),
			leaf:   ObjectItemPath(1, "bucket", "prefix/file.csv"),
			run:    RequireObjectLeafPath,
			want:   "bucket/prefix/file.csv",
		},
		{
			name:   "file",
			root:   FileRootPath(1),
			branch: FileDirectoryPath(1, "dir"),
			leaf:   FileItemPath(1, "dir/file.csv"),
			run:    RequireFileLeafPath,
			want:   "dir/file.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/empty", func(t *testing.T) {
			if _, err := tt.run(EngineCatalogPath{}); err == nil {
				t.Fatal("helper accepted empty content path")
			}
		})
		t.Run(tt.name+"/root", func(t *testing.T) {
			if _, err := tt.run(tt.root); err == nil {
				t.Fatal("helper accepted root content path")
			}
		})
		t.Run(tt.name+"/branch", func(t *testing.T) {
			if _, err := tt.run(tt.branch); err == nil {
				t.Fatal("helper accepted branch content path")
			}
		})
		t.Run(tt.name+"/leaf", func(t *testing.T) {
			got, err := tt.run(tt.leaf)
			if err != nil {
				t.Fatalf("helper rejected leaf content path: %v", err)
			}
			if got != tt.want {
				t.Fatalf("content path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCatalogResolverHelpersRejectEmptyPath(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "object",
			run: func() error {
				_, err := ResolveObjectCatalogPath(ctx, ObjectCatalogCallbacks{}, nil, 1, EngineCatalogPath{})
				return err
			},
		},
		{
			name: "file",
			run: func() error {
				_, err := ResolveFileCatalogPath(ctx, FileCatalogCallbacks{}, nil, 1, EngineCatalogPath{})
				return err
			},
		},
		{
			name: "tabular",
			run: func() error {
				_, err := ResolveTabularCatalogPath(ctx, minimalTabularCatalogCallbacks(), &Engine{ID: 1, EngineType: "unregistered"}, EngineCatalogPath{})
				return err
			},
		},
		{
			name: "dynamic-schema",
			run: func() error {
				_, err := ResolveDynamicSchemaCatalogPath(ctx, DynamicSchemaCatalogCallbacks{}, 1, nil, EngineCatalogPath{})
				return err
			},
		},
		{
			name: "graph",
			run: func() error {
				_, err := ResolveGraphCatalogPath(ctx, GraphCatalogCallbacks{}, 1, nil, EngineCatalogPath{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("resolver accepted empty path")
			}
		})
	}
}

func TestCatalogResolverHelpersAcceptExplicitRootPath(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		root EngineCatalogPath
		run  func(EngineCatalogPath) (*EngineCatalogEntry, error)
	}{
		{
			name: "object",
			root: ObjectRootPath(1),
			run: func(path EngineCatalogPath) (*EngineCatalogEntry, error) {
				return ResolveObjectCatalogPath(ctx, ObjectCatalogCallbacks{}, nil, 1, path)
			},
		},
		{
			name: "file",
			root: FileRootPath(1),
			run: func(path EngineCatalogPath) (*EngineCatalogEntry, error) {
				return ResolveFileCatalogPath(ctx, FileCatalogCallbacks{}, nil, 1, path)
			},
		},
		{
			name: "tabular",
			root: EngineCatalogRootPath(TabularCatalogModel(EngineCatalogTermDatabase), 1),
			run: func(path EngineCatalogPath) (*EngineCatalogEntry, error) {
				return ResolveTabularCatalogPath(ctx, minimalTabularCatalogCallbacks(), &Engine{ID: 1, EngineType: "unregistered"}, path)
			},
		},
		{
			name: "dynamic-schema",
			root: EngineCatalogRootPath(DynamicSchemaCatalogModel(), 1),
			run: func(path EngineCatalogPath) (*EngineCatalogEntry, error) {
				return ResolveDynamicSchemaCatalogPath(ctx, DynamicSchemaCatalogCallbacks{}, 1, nil, path)
			},
		},
		{
			name: "graph",
			root: EngineCatalogRootPath(GraphCatalogModel(), 1),
			run: func(path EngineCatalogPath) (*EngineCatalogEntry, error) {
				return ResolveGraphCatalogPath(ctx, GraphCatalogCallbacks{}, 1, nil, path)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := tt.run(tt.root)
			if err != nil {
				t.Fatalf("Resolve(root) error = %v", err)
			}
			if entry == nil || entry.Role != EngineCatalogRoleBranch || !IsEngineCatalogRootPath(entry.Path) {
				t.Fatalf("root entry = %#v", entry)
			}
		})
	}
}

func TestCatalogProviderHelpersListExplicitRoot(t *testing.T) {
	ctx := context.Background()

	objectRoot := ObjectRootPath(1)
	objectCalled := false
	if _, err := ListObjectCatalogChildren(ctx, ObjectCatalogCallbacks{
		ListBucketsFunc: func(_ context.Context, _ ConnectionInfo, root EngineCatalogPath) ([]EngineCatalogEntry, error) {
			objectCalled = IsEngineCatalogRootPath(root)
			return nil, nil
		},
	}, nil, 1, objectRoot, ListOptions{}); err != nil {
		t.Fatalf("ListObjectCatalogChildren(root) error = %v", err)
	}
	if !objectCalled {
		t.Fatal("object root callback was not called with explicit root")
	}

	fileRoot := FileRootPath(1)
	fileCalled := false
	if _, err := ListFileCatalogChildren(ctx, FileCatalogCallbacks{
		ListDirectoryFunc: func(_ context.Context, _ ConnectionInfo, root EngineCatalogPath) ([]EngineCatalogEntry, error) {
			fileCalled = IsEngineCatalogRootPath(root)
			return nil, nil
		},
	}, nil, 1, fileRoot, ListOptions{}); err != nil {
		t.Fatalf("ListFileCatalogChildren(root) error = %v", err)
	}
	if !fileCalled {
		t.Fatal("file root callback was not called with explicit root")
	}
}

func minimalTabularCatalogCallbacks() TabularCatalogCallbacks {
	return TabularCatalogCallbacks{
		ListNamespaces: func(context.Context, *gorm.DB, EngineCatalogPath) ([]EngineCatalogEntry, error) {
			return nil, nil
		},
		ListTables: func(context.Context, *gorm.DB, string) ([]datatype.TableInfo, error) {
			return nil, nil
		},
		ListColumns: func(context.Context, *gorm.DB, string, string) ([]datatype.FieldInfo, error) {
			return nil, nil
		},
	}
}
