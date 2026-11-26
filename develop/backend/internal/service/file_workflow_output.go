package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format/plugins/sqlite"
)

func (b *fileWorkflowInputs) downloadOutput(ctx context.Context, index int) (*os.File, plugin.WorkflowPrivateFile, error) {
	var receipt plugin.WorkflowPrivateFile
	if index < 0 || index != b.nextOutput || index >= len(b.manifest.Outputs) || b.runtime == nil || b.live() != nil {
		return nil, receipt, errPrivatePublication
	}
	// A failed or uncertain download ends this sequence.
	b.nextOutput = -1
	bounded, stop := b.bounded(ctx)
	defer stop()
	status, err := b.runtime.Status(bounded)
	if err != nil {
		return nil, receipt, errPrivatePublication
	}
	var state struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(status.Execution, &state) != nil || state.Status != "success" || len(status.Outputs) != len(b.manifest.Outputs) {
		return nil, receipt, errPrivatePublication
	}
	receipt = status.Outputs[index]
	file, err := os.OpenFile(filepath.Join(b.directory, "output-"+strconv.Itoa(index)+".gpkg"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, receipt, errPrivatePublication
	}
	copyErr := b.runtime.CopyOutput(bounded, index, receipt, file)
	syncErr := file.Sync()
	if err = errors.Join(copyErr, syncErr); err == nil {
		err = sqlite.ValidateStandaloneGeoPackage(bounded, file.Name())
	}
	if err == nil {
		err = b.live()
	}
	if err == nil {
		_, err = file.Seek(0, 0)
	}
	if err != nil {
		_ = file.Close()
		return nil, receipt, errPrivatePublication
	}
	b.nextOutput = index + 1
	return file, receipt, nil
}
