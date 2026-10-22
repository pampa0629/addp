package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/system/i18n"
	"github.com/addp/system/internal/auditarchive"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if e := run(ctx, os.Args[1:], os.Stdout); e != nil {
		// Never print wrapped network, SQL, credential or private configuration errors.
		lang := commoni18n.LangZhCN
		locale := os.Getenv("LC_ALL")
		if locale == "" {
			locale = os.Getenv("LANG")
		}
		if strings.HasPrefix(strings.ToLower(locale), "en") {
			lang = commoni18n.LangEn
		}
		fmt.Fprintln(os.Stderr, commoni18n.ForLanguage(lang, "system.audit_archive.failed"))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return auditarchive.ErrUnknown
	}
	action := args[0]
	if action != "init" && action != "collect" && action != "verify" && action != "query" && action != "plan" && action != "receiver-init" && action != "receive" && action != "receiver-verify" {
		return auditarchive.ErrUnknown
	}
	flags := flag.NewFlagSet("audit-archive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configFile := flags.String("config", "", "")
	tenant := flags.String("tenant-id", "", "")
	event := flags.String("event-name", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *configFile == "" {
		return auditarchive.ErrUnknown
	}
	if action != "query" && (*tenant != "" || *event != "") {
		return auditarchive.ErrUnknown
	}
	c, e := auditarchive.ReadConfig(*configFile)
	if e != nil {
		return e
	}
	expectedMode := "auditor"
	switch action {
	case "init", "collect":
		expectedMode = "collector"
	case "receiver-init", "receive":
		expectedMode = "receiver"
	case "receiver-verify":
		expectedMode = "receiver-auditor"
	}
	if c.Mode != expectedMode {
		return auditarchive.ErrUnknown
	}
	store, e := c.Store()
	if e != nil {
		return e
	}
	switch action {
	case "receiver-init":
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		root, e := auditarchive.InitReceiver(ctx, store, c.SourceID, c.DeploymentID)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(root)
	case "receiver-verify":
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result, e := auditarchive.VerifyReceiver(ctx, store, c.SourceID, c.DeploymentID, c.Root, c.Checkpoint)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(result)
	case "receive":
		return auditarchive.ServeReceiver(ctx, c, store)
	case "init":
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		root, initErr := c.PostgreSQL().Initialize(ctx, store)
		if initErr != nil {
			e = initErr
			return e
		}
		return json.NewEncoder(out).Encode(struct {
			State  string           `json:"state"`
			Source string           `json:"source_id"`
			Root   auditarchive.Ref `json:"root"`
		}{"initialized", c.SourceID, root})
	case "collect":
		if !c.RootValid() {
			return auditarchive.ErrUnknown
		}
		return c.PostgreSQL().Collect(ctx, store, func(chain *auditarchive.Chain) {
			// Last event age is not replication lag: an idle source may have no events.
			delay := chain.ArchivedAt.Sub(chain.LastCommitAt).Milliseconds()
			state := "observed"
			if delay < 0 {
				state = "unknown"
			}
			if delay > 5*time.Minute.Milliseconds() {
				state = "delayed"
			}
			_ = json.NewEncoder(out).Encode(struct {
				State      string              `json:"state"`
				Lag        string              `json:"replication_lag"`
				DelayState string              `json:"commit_delay_state"`
				Delay      int64               `json:"observed_commit_delay_ms"`
				Checkpoint *auditarchive.Chain `json:"checkpoint"`
			}{"archived", "unknown", state, delay, chain})
		})
	case "plan":
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		plan, e := auditarchive.PlanExpiry(ctx, store, c.Root)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(plan)
	default:
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		chain, rows, e := auditarchive.QueryArchive(ctx, store, store, c.Root, c.Actor, c.RetentionDays, auditarchive.Query{TenantID: *tenant, EventName: *event}, action == "verify")
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(struct {
			State      string              `json:"state"`
			Checkpoint *auditarchive.Chain `json:"checkpoint"`
			Rows       []auditarchive.Row  `json:"rows,omitempty"`
		}{"verified_retained_scope", chain, rows})
	}
}
