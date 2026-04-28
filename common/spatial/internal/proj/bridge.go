//go:build proj

package proj

/*
#cgo pkg-config: proj
#include <stdlib.h>
#include <proj.h>
*/
import "C"

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"
)

type Transformer struct {
	ctx *C.PJ_CONTEXT
	pj  *C.PJ
}

func NewTransformer(sourceCRS, targetCRS string) (*Transformer, error) {
	if sourceCRS == "" || targetCRS == "" {
		return nil, fmt.Errorf("source/target crs is empty")
	}

	ctx := C.proj_context_create()
	if ctx == nil {
		return nil, fmt.Errorf("create proj context failed")
	}

	if err := configureContext(ctx); err != nil {
		C.proj_context_destroy(ctx)
		return nil, err
	}

	source := C.CString(sourceCRS)
	target := C.CString(targetCRS)
	defer C.free(unsafe.Pointer(source))
	defer C.free(unsafe.Pointer(target))

	raw := C.proj_create_crs_to_crs(ctx, source, target, nil)
	if raw == nil {
		err := contextError(ctx)
		C.proj_context_destroy(ctx)
		return nil, fmt.Errorf("create transformation failed: %s", err)
	}

	normalized := C.proj_normalize_for_visualization(ctx, raw)
	C.proj_destroy(raw)
	if normalized == nil {
		err := contextError(ctx)
		C.proj_context_destroy(ctx)
		return nil, fmt.Errorf("normalize transformation failed: %s", err)
	}

	return &Transformer{
		ctx: ctx,
		pj:  normalized,
	}, nil
}

func (t *Transformer) Close() {
	if t == nil {
		return
	}
	if t.pj != nil {
		C.proj_destroy(t.pj)
		t.pj = nil
	}
	if t.ctx != nil {
		C.proj_context_destroy(t.ctx)
		t.ctx = nil
	}
}

func (t *Transformer) TransformFlatCoords(flatCoords []float64, stride int) ([]float64, error) {
	if t == nil || t.pj == nil {
		return nil, fmt.Errorf("transformer is not initialized")
	}
	if stride < 2 {
		return nil, fmt.Errorf("unsupported stride: %d", stride)
	}
	if len(flatCoords) == 0 {
		return nil, nil
	}
	if len(flatCoords)%stride != 0 {
		return nil, fmt.Errorf("flat coordinates length %d is not aligned to stride %d", len(flatCoords), stride)
	}

	out := append([]float64(nil), flatCoords...)
	count := len(out) / stride

	var xPtr *float64
	var yPtr *float64
	var zPtr *float64

	if len(out) > 0 {
		xPtr = &out[0]
		yPtr = &out[1]
		if stride > 2 {
			zPtr = &out[2]
		}
	}

	transformed := C.proj_trans_generic(
		t.pj,
		C.PJ_FWD,
		(*C.double)(unsafe.Pointer(xPtr)), C.size_t(stride*8), C.size_t(count),
		(*C.double)(unsafe.Pointer(yPtr)), C.size_t(stride*8), C.size_t(count),
		(*C.double)(unsafe.Pointer(zPtr)), C.size_t(stride*8), C.size_t(count),
		nil, 0, 0,
	)
	if int(transformed) != count {
		return nil, fmt.Errorf("proj transformed %d/%d coordinates: %s", int(transformed), count, t.lastError())
	}
	return out, nil
}

func (t *Transformer) lastError() string {
	if t == nil || t.ctx == nil {
		return "unknown error"
	}
	return contextError(t.ctx)
}

func contextError(ctx *C.PJ_CONTEXT) string {
	if ctx == nil {
		return "unknown error"
	}

	code := C.proj_context_errno(ctx)
	if code == 0 {
		return "unknown error"
	}

	msg := C.proj_context_errno_string(ctx, code)
	if msg == nil {
		return fmt.Sprintf("proj error code %d", int(code))
	}
	return C.GoString(msg)
}

func configureContext(ctx *C.PJ_CONTEXT) error {
	dataDir, err := detectPROJDataDir()
	if err != nil {
		return err
	}

	dbPath := filepath.Join(dataDir, "proj.db")
	cDBPath := C.CString(dbPath)
	defer C.free(unsafe.Pointer(cDBPath))

	cDataDir := C.CString(dataDir)
	defer C.free(unsafe.Pointer(cDataDir))

	paths := []*C.char{cDataDir, nil}
	if C.proj_context_set_database_path(ctx, cDBPath, nil, nil) == 0 {
		return fmt.Errorf("set proj database path failed: %s", contextError(ctx))
	}
	C.proj_context_set_search_paths(ctx, 1, (**C.char)(unsafe.Pointer(&paths[0])))
	return nil
}

func detectPROJDataDir() (string, error) {
	output, err := exec.Command("pkg-config", "--variable=datadir", "proj").Output()
	if err != nil {
		return "", fmt.Errorf("detect proj datadir failed: %w", err)
	}

	dir := strings.TrimSpace(string(output))
	if dir == "" {
		return "", fmt.Errorf("proj datadir is empty")
	}
	return dir, nil
}
