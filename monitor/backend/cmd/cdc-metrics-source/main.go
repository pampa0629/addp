// cdc-metrics-source is a private deployment source, separate from Monitor Web.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/addp/monitor/internal/cdcsource"
	"github.com/addp/monitor/internal/sourceserver"
)

func configuration(r io.Reader) (cdcsource.Target, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1025))
	if err != nil || len(raw) > 1024 {
		return cdcsource.Target{}, cdcsource.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var target cdcsource.Target
	if decoder.Decode(&target) != nil || !target.Valid() {
		return cdcsource.Target{}, cdcsource.ErrUnavailable
	}
	if _, err := decoder.Token(); err != io.EOF {
		return cdcsource.Target{}, cdcsource.ErrUnavailable
	}
	return target, nil
}

func run() error {
	if len(os.Args) != 1 {
		return errors.New("unexpected source arguments")
	}
	file, err := os.Open("/source/config.json")
	if err != nil {
		return cdcsource.ErrUnavailable
	}
	defer file.Close()
	target, err := configuration(file)
	if err != nil {
		return err
	}
	return sourceserver.Serve(cdcsource.Handler(target), "/source")
}

func main() {
	if run() != nil {
		_, _ = io.WriteString(os.Stderr, "CDC observation source unavailable\n")
		os.Exit(1)
	}
}
