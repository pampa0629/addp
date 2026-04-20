package nfs

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
)

// poolKey NFS 连接池键
type poolKey struct {
	server     string
	exportPath string
}

// poolEntry 连接池条目
type poolEntry struct {
	mount     *gonfs.Mount
	target    *gonfs.Target
	createdAt time.Time
}

var (
	pool   = make(map[poolKey]*poolEntry)
	poolMu sync.Mutex
)

const connTTL = 5 * time.Minute

// getOrCreateMount 获取或创建 NFS 挂载连接（带连接池）
func getOrCreateMount(server, exportPath string) (*gonfs.Mount, *gonfs.Target, error) {
	key := poolKey{server: server, exportPath: exportPath}

	poolMu.Lock()
	defer poolMu.Unlock()

	if entry, ok := pool[key]; ok {
		if time.Since(entry.createdAt) < connTTL {
			return entry.mount, entry.target, nil
		}
		// 过期，关闭旧连接
		_ = entry.mount.Unmount()
		_ = entry.mount.Close()
		delete(pool, key)
	}

	mount, target, err := dialNFS(server, exportPath)
	if err != nil {
		return nil, nil, err
	}

	pool[key] = &poolEntry{
		mount:     mount,
		target:    target,
		createdAt: time.Now(),
	}
	return mount, target, nil
}

// dialNFS 建立 NFS 连接
func dialNFS(server, exportPath string) (*gonfs.Mount, *gonfs.Target, error) {
	// 解析服务器地址
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		// 没有端口，直接使用
		host = server
	}

	mount, err := gonfs.DialMount(host)
	if err != nil {
		return nil, nil, fmt.Errorf("dial NFS mount %s: %w", host, err)
	}

	auth := rpc.NewAuthUnix("addp-nfs-client", uint32(os.Getuid()), uint32(os.Getgid()))
	target, err := mount.Mount(exportPath, auth.Auth())
	if err != nil {
		_ = mount.Close()
		return nil, nil, fmt.Errorf("mount %s:%s: %w", host, exportPath, err)
	}

	return mount, target, nil
}

// invalidatePool 使指定连接的缓存失效（连接出错时调用）
func invalidatePool(server, exportPath string) {
	key := poolKey{server: server, exportPath: exportPath}
	poolMu.Lock()
	defer poolMu.Unlock()
	if entry, ok := pool[key]; ok {
		_ = entry.mount.Unmount()
		_ = entry.mount.Close()
		delete(pool, key)
	}
}
