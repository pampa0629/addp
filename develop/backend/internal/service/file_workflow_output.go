package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format/plugins/sqlite"
)

func (b *fileWorkflowInputs) downloadOutput(ctx context.Context) (*os.File, plugin.WorkflowPrivateFile, error) {
	var receipt plugin.WorkflowPrivateFile
	if b.outputAttempted || b.runtime == nil || len(b.manifest.Outputs) != 1 || b.live() != nil {
		return nil, receipt, errPrivatePublication
	}
	b.outputAttempted = true
	bounded, stop := b.bounded(ctx)
	defer stop()
	status, err := b.runtime.Status(bounded)
	if err != nil {
		return nil, receipt, errPrivatePublication
	}
	var state struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(status.Execution, &state) != nil || state.Status != "success" || len(status.Outputs) != 1 {
		return nil, receipt, errPrivatePublication
	}
	receipt = status.Outputs[0]
	file, err := os.OpenFile(filepath.Join(b.directory, "output-0.gpkg"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, receipt, errPrivatePublication
	}
	copyErr := b.runtime.CopyOutput(bounded, 0, receipt, file)
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
	return file, receipt, nil
}
