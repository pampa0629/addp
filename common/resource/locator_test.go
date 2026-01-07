package resource

import (
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      *ResourceLocator
		wantError bool
	}{
		{
			name: "PostgreSQL 表",
			uri:  "addp://engine/1/path/public/users?type=table",
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "users"},
				Type:     TypeTable,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "PostgreSQL 表 with meta_id",
			uri:  "addp://engine/1/path/public/users?type=table&meta_id=100",
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "users"},
				Type:     TypeTable,
				MetaID:   uintPtr(100),
			},
			wantError: false,
		},
		{
			name: "MongoDB 集合",
			uri:  "addp://engine/2/path/business/orders?type=collection",
			want: &ResourceLocator{
				EngineID: 2,
				Path:     []string{"business", "orders"},
				Type:     TypeCollection,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "MinIO 对象",
			uri:  "addp://engine/3/path/uploads/2024/geo/data.shp?type=object",
			want: &ResourceLocator{
				EngineID: 3,
				Path:     []string{"uploads", "2024", "geo", "data.shp"},
				Type:     TypeObject,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "MinIO 目录",
			uri:  "addp://engine/3/path/uploads/2024?type=directory",
			want: &ResourceLocator{
				EngineID: 3,
				Path:     []string{"uploads", "2024"},
				Type:     TypeDirectory,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "空路径（引擎根节点）",
			uri:  "addp://engine/1/path/?type=database",
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{},
				Type:     TypeDatabase,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "特殊字符（空格）",
			uri:  "addp://engine/1/path/public/user%20files?type=table",
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "user files"},
				Type:     TypeTable,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name: "中文路径",
			uri:  "addp://engine/1/path/%E6%95%B0%E6%8D%AE%E5%BA%93/%E7%94%A8%E6%88%B7%E8%A1%A8?type=table",
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"数据库", "用户表"},
				Type:     TypeTable,
				MetaID:   nil,
			},
			wantError: false,
		},
		{
			name:      "错误的协议",
			uri:       "http://engine/1/path/public/users?type=table",
			want:      nil,
			wantError: true,
		},
		{
			name:      "缺少 type 参数",
			uri:       "addp://engine/1/path/public/users",
			want:      nil,
			wantError: true,
		},
		{
			name:      "无效的 engine_id",
			uri:       "addp://engine/invalid/path/public/users?type=table",
			want:      nil,
			wantError: true,
		},
		{
			name:      "错误的路径格式",
			uri:       "addp://engine/1/invalid/public/users?type=table",
			want:      nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURI(tt.uri)
			if (err != nil) != tt.wantError {
				t.Errorf("ParseURI() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if tt.wantError {
				return
			}
			if !equalResourceLocator(got, tt.want) {
				t.Errorf("ParseURI() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestToURI(t *testing.T) {
	tests := []struct {
		name string
		loc  *ResourceLocator
		want string
	}{
		{
			name: "PostgreSQL 表",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "users"},
				Type:     TypeTable,
			},
			want: "addp://engine/1/path/public/users?type=table",
		},
		{
			name: "PostgreSQL 表 with meta_id",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "users"},
				Type:     TypeTable,
				MetaID:   uintPtr(100),
			},
			want: "addp://engine/1/path/public/users?type=table&meta_id=100",
		},
		{
			name: "MinIO 深层路径",
			loc: &ResourceLocator{
				EngineID: 3,
				Path:     []string{"uploads", "2024", "geo", "data.shp"},
				Type:     TypeObject,
			},
			want: "addp://engine/3/path/uploads/2024/geo/data.shp?type=object",
		},
		{
			name: "特殊字符（空格）",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "user files"},
				Type:     TypeTable,
			},
			want: "addp://engine/1/path/public/user%20files?type=table",
		},
		{
			name: "空路径",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{},
				Type:     TypeDatabase,
			},
			want: "addp://engine/1/path/?type=database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.ToURI()
			if got != tt.want {
				t.Errorf("ToURI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathString(t *testing.T) {
	tests := []struct {
		name string
		loc  *ResourceLocator
		want string
	}{
		{
			name: "两级路径",
			loc: &ResourceLocator{
				Path: []string{"public", "users"},
			},
			want: "public/users",
		},
		{
			name: "四级路径",
			loc: &ResourceLocator{
				Path: []string{"uploads", "2024", "geo", "data.shp"},
			},
			want: "uploads/2024/geo/data.shp",
		},
		{
			name: "空路径",
			loc: &ResourceLocator{
				Path: []string{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.PathString()
			if got != tt.want {
				t.Errorf("PathString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFullName(t *testing.T) {
	tests := []struct {
		name string
		loc  *ResourceLocator
		want string
	}{
		{
			name: "表（schema.table 格式）",
			loc: &ResourceLocator{
				Path: []string{"public", "users"},
				Type: TypeTable,
			},
			want: "public.users",
		},
		{
			name: "集合（database.collection 格式）",
			loc: &ResourceLocator{
				Path: []string{"business", "orders"},
				Type: TypeCollection,
			},
			want: "business.orders",
		},
		{
			name: "对象（bucket/path 格式）",
			loc: &ResourceLocator{
				Path: []string{"uploads", "2024", "data.shp"},
				Type: TypeObject,
			},
			want: "uploads/2024/data.shp",
		},
		{
			name: "目录（bucket/path 格式）",
			loc: &ResourceLocator{
				Path: []string{"uploads", "2024"},
				Type: TypeDirectory,
			},
			want: "uploads/2024",
		},
		{
			name: "表（单级路径）",
			loc: &ResourceLocator{
				Path: []string{"users"},
				Type: TypeTable,
			},
			want: "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.FullName()
			if got != tt.want {
				t.Errorf("FullName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLastSegment(t *testing.T) {
	tests := []struct {
		name string
		loc  *ResourceLocator
		want string
	}{
		{
			name: "多级路径",
			loc: &ResourceLocator{
				Path: []string{"public", "users"},
			},
			want: "users",
		},
		{
			name: "单级路径",
			loc: &ResourceLocator{
				Path: []string{"public"},
			},
			want: "public",
		},
		{
			name: "空路径",
			loc: &ResourceLocator{
				Path: []string{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.LastSegment()
			if got != tt.want {
				t.Errorf("LastSegment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParentPath(t *testing.T) {
	tests := []struct {
		name string
		loc  *ResourceLocator
		want *ResourceLocator
	}{
		{
			name: "表的父路径（schema）",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public", "users"},
				Type:     TypeTable,
			},
			want: &ResourceLocator{
				EngineID: 1,
				Path:     []string{"public"},
				Type:     TypeSchema,
				MetaID:   nil,
			},
		},
		{
			name: "集合的父路径（database）",
			loc: &ResourceLocator{
				EngineID: 2,
				Path:     []string{"business", "orders"},
				Type:     TypeCollection,
			},
			want: &ResourceLocator{
				EngineID: 2,
				Path:     []string{"business"},
				Type:     TypeDatabase,
				MetaID:   nil,
			},
		},
		{
			name: "对象的父路径（directory）",
			loc: &ResourceLocator{
				EngineID: 3,
				Path:     []string{"uploads", "2024", "data.shp"},
				Type:     TypeObject,
			},
			want: &ResourceLocator{
				EngineID: 3,
				Path:     []string{"uploads", "2024"},
				Type:     TypeDirectory,
				MetaID:   nil,
			},
		},
		{
			name: "空路径",
			loc: &ResourceLocator{
				EngineID: 1,
				Path:     []string{},
				Type:     TypeDatabase,
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.ParentPath()
			if !equalResourceLocator(got, tt.want) {
				t.Errorf("ParentPath() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestClone(t *testing.T) {
	original := &ResourceLocator{
		EngineID: 1,
		Path:     []string{"public", "users"},
		Type:     TypeTable,
		MetaID:   uintPtr(100),
	}

	cloned := original.Clone()

	// 验证值相等
	if !equalResourceLocator(original, cloned) {
		t.Errorf("Clone() = %+v, want %+v", cloned, original)
	}

	// 验证是深拷贝（修改 cloned 不影响 original）
	cloned.Path[0] = "private"
	if original.Path[0] == "private" {
		t.Error("Clone() should create a deep copy, but modifying clone affected original")
	}

	*cloned.MetaID = 200
	if *original.MetaID == 200 {
		t.Error("Clone() should create a deep copy of MetaID, but modifying clone affected original")
	}
}

func TestRoundTrip(t *testing.T) {
	// 测试 ToURI -> ParseURI 往返转换
	tests := []*ResourceLocator{
		{
			EngineID: 1,
			Path:     []string{"public", "users"},
			Type:     TypeTable,
		},
		{
			EngineID: 2,
			Path:     []string{"business", "orders"},
			Type:     TypeCollection,
			MetaID:   uintPtr(100),
		},
		{
			EngineID: 3,
			Path:     []string{"uploads", "2024", "geo", "data.shp"},
			Type:     TypeObject,
		},
		{
			EngineID: 1,
			Path:     []string{"数据库", "用户表"},
			Type:     TypeTable,
		},
	}

	for _, original := range tests {
		t.Run(original.FullName(), func(t *testing.T) {
			uri := original.ToURI()
			parsed, err := ParseURI(uri)
			if err != nil {
				t.Fatalf("ParseURI() error = %v", err)
			}
			if !equalResourceLocator(original, parsed) {
				t.Errorf("Round trip failed: original = %+v, parsed = %+v", original, parsed)
			}
		})
	}
}

// 辅助函数

func uintPtr(n uint) *uint {
	return &n
}

func equalResourceLocator(a, b *ResourceLocator) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.EngineID != b.EngineID || a.Type != b.Type {
		return false
	}

	if len(a.Path) != len(b.Path) {
		return false
	}
	for i := range a.Path {
		if a.Path[i] != b.Path[i] {
			return false
		}
	}

	if (a.MetaID == nil) != (b.MetaID == nil) {
		return false
	}
	if a.MetaID != nil && *a.MetaID != *b.MetaID {
		return false
	}

	return true
}
