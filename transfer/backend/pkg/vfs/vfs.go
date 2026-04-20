package vfs

import (
	"io"
	"os"
)

// VFS 为格式写入器提供统一的文件写入接口，屏蔽底层存储引擎差异
type VFS interface {
	Create(path string) (io.WriteCloser, error)
	MkdirAll(path string) error
}

// LocalVFS 封装本地文件系统操作
type LocalVFS struct{}

func (v *LocalVFS) Create(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func (v *LocalVFS) MkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}
