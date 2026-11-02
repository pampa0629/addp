// Deployment-only status update. Built with the exact mc source module's pinned SDK.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/minio/madmin-go/v3"
)

//go:embed policy.json
var policy []byte

type request struct {
	RootAccess string `json:"root_access"`
	RootSecret string `json:"root_secret"`
	AccessKey  string `json:"access_key"`
	Enabled    *bool  `json:"enabled"`
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("addp.minio-observation-status/v1")
		return nil
	}
	if len(os.Args) != 1 {
		return fmt.Errorf("unexpected arguments")
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 16385))
	if err != nil || len(data) > 16384 {
		return fmt.Errorf("input budget")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input request
	if err = decoder.Decode(&input); err != nil {
		return fmt.Errorf("invalid input")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || input.Enabled == nil || len(input.RootAccess) < 3 || len(input.RootSecret) < 8 ||
		!regexp.MustCompile(`^ADDP[A-F0-9]{16}$`).MatchString(input.AccessKey) {
		return fmt.Errorf("invalid identity")
	}
	client, err := madmin.New("127.0.0.1:9000", input.RootAccess, input.RootSecret, false)
	if err != nil {
		return fmt.Errorf("client unavailable")
	}
	status := "off"
	if *input.Enabled {
		status = "on"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = client.UpdateServiceAccount(ctx, input.AccessKey, madmin.UpdateServiceAccountReq{NewStatus: status, NewPolicy: policy}); err != nil {
		return fmt.Errorf("native update rejected")
	}
	fmt.Println(`{"status":"success"}`)
	return nil
}

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "MinIO observation status update rejected")
		os.Exit(1)
	}
}
