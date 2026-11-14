package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Both commands are short-lived deployment processes. The server never reads their stdin.
func runModuleDeployment(args []string, input io.Reader) error {
	if len(args) != 2 {
		return errors.New("expected deployment command and module name")
	}
	module := args[1]
	reader := json.NewDecoder(input)
	reader.DisallowUnknownFields()
	var request struct {
		Secret      string          `json:"secret"`
		Declaration json.RawMessage `json:"declaration"`
	}
	if err := reader.Decode(&request); err != nil {
		return errors.New("invalid deployment input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientID := "addp-" + module + "-publisher"
	switch args[0] {
	case "provision-module-publisher":
		cfg := config.Load()
		db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{})
		if err != nil {
			return err
		}
		provisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
		if err != nil {
			return err
		}
		return provisioner.ProvisionPublisher(ctx, clientID, request.Secret)
	case "publish-module-declaration":
		var artifact models.ModuleDeclarationCreate
		decoder := json.NewDecoder(strings.NewReader(string(request.Declaration)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&artifact); err != nil || artifact.ModuleName != module || artifact.RoutePrefix == nil {
			return errors.New("invalid module declaration artifact")
		}
		// System canonicalizes entries by their stable ID on publication.
		if artifact.ConfigurationManagement != nil {
			sort.Slice(artifact.ConfigurationManagement.Entries, func(i, j int) bool {
				return artifact.ConfigurationManagement.Entries[i].ID < artifact.ConfigurationManagement.Entries[j].ID
			})
		}
		tokens, err := commonclient.NewOAuthServiceTokenSource(os.Getenv("SYSTEM_URL"), clientID, request.Secret, nil)
		if err != nil {
			return err
		}
		client := commonclient.NewSystemServiceClient(os.Getenv("SYSTEM_URL"), tokens, nil)
		return publishModuleDeclaration(ctx, client, artifact)
	default:
		return errors.New("unknown module deployment command")
	}
}

func publishModuleDeclaration(ctx context.Context, client *commonclient.SystemServiceClient, artifact models.ModuleDeclarationCreate) error {
	var current models.ModuleDeclarationView
	matches := func() bool {
		if current.DeclarationPublishedAt == nil || current.RoutePrefix != *artifact.RoutePrefix {
			return false
		}
		var configuration, provider any
		if artifact.ConfigurationManagement != nil {
			configuration = artifact.ConfigurationManagement
		}
		if artifact.TaskProvider != nil {
			provider = artifact.TaskProvider
		}
		return sameJSON(current.ConfigurationManagement, configuration) && sameJSON(current.TaskProvider, provider)
	}
	err := client.GetModuleDeclaration(ctx, artifact.ModuleName, &current)
	status, _ := commonclient.SystemAPIStatusCode(err)
	switch {
	case status == http.StatusNotFound:
		err = client.CreateModuleDeclaration(ctx, artifact)
	case err != nil:
		return err
	case matches():
		return nil
	default:
		err = client.UpdateModuleDeclaration(ctx, artifact.ModuleName, models.ModuleDeclarationUpdate{Version: current.Version, ModuleDeclarationFields: artifact.ModuleDeclarationFields})
	}
	code, _ := commonclient.SystemAPIErrorCode(err)
	if code != "resource_version_conflict" {
		return err
	}
	expected := current.Version
	if readErr := client.GetModuleDeclaration(ctx, artifact.ModuleName, &current); readErr != nil {
		return readErr
	}
	if matches() {
		return nil
	}
	return fmt.Errorf("module %s declaration conflict: expected version %d, current version %d", artifact.ModuleName, expected, current.Version)
}

func sameJSON(stored []byte, target any) bool {
	if target == nil {
		return len(stored) == 0 || string(stored) == "null"
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return false
	}
	var left, right any
	a, b := json.NewDecoder(bytes.NewReader(raw)), json.NewDecoder(bytes.NewReader(stored))
	a.UseNumber()
	b.UseNumber()
	return a.Decode(&left) == nil && b.Decode(&right) == nil && reflect.DeepEqual(left, right)
}
