package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/logpipeline"
	"github.com/addp/common/runtimelog"
	"github.com/google/uuid"
)

func observe() error {
	flags := flag.NewFlagSet("observe", flag.ContinueOnError)
	once := flags.Bool("once", false, "sample and report once")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("unexpected observer argument")
	}

	node := os.Getenv("ADDP_HOST_NODE_NAME")
	if !logpipeline.Identity.MatchString(node) {
		return fmt.Errorf("observer requires explicit ADDP_HOST_NODE_NAME")
	}
	o, err := runtimelog.FromEnvironment("housekeeper", "backend", "observer")
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	source, err := client.NewOAuthServiceTokenSource(os.Getenv("SYSTEM_URL"), "addp-log-observer", os.Getenv("LOG_OBSERVER_SERVICE_CLIENT_SECRET"), httpClient)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(os.Getenv("MONITOR_URL"), "/")
	if endpoint == "" {
		return fmt.Errorf("observer requires MONITOR_URL")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	boot := uuid.NewString()
	probe := runtimelog.NewProbeReceiver(o)
	defer probe.Close()
	var seq uint64
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		seq++
		catalog := runtimelog.DiscoverSources(o, node, boot, seq)
		catalogAccepted := deliverReport(ctx, httpClient, source, strings.TrimRight(os.Getenv("SYSTEM_URL"), "/")+"/api/v1/system/runtime/module-log-source-observations", catalog)
		obs := runtimelog.Observe(ctx, o, probe, node, boot, seq, os.Getenv("LOKI_URL"), os.Getenv("LOKI_READ_TOKEN"), os.Getenv("ALLOY_URL"))
		accepted := deliverReport(ctx, httpClient, source, endpoint+"/api/v1/monitor/platform/log-observations", obs)
		if *once {
			if !accepted || !catalogAccepted {
				return fmt.Errorf("log observation not accepted")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Metadata and health delivery remain independent; errors never include the body or credentials.
func deliverReport(ctx context.Context, httpClient *http.Client, source *client.OAuthServiceTokenSource, endpoint string, value any) bool {
	body, err := json.Marshal(value)
	if err != nil {
		return false
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := source.PlatformToken(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "log observer authentication unavailable")
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			break
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := httpClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
			if res.StatusCode == 200 {
				return true
			}
			if res.StatusCode == 401 {
				source.InvalidatePlatformToken(token)
				continue
			}
			if res.StatusCode < 500 {
				fmt.Fprintln(os.Stderr, "log observer report rejected")
				break
			}
		}
	}
	fmt.Fprintln(os.Stderr, "log observer report not accepted")
	return false
}
