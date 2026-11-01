// redis-metrics-source is a deployment source, separate from Monitor Web.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/addp/monitor/internal/redissource"
)

type configuration struct {
	Address  string `json:"address"`
	Scope    string `json:"scope"`
	Password string `json:"password"`
	Owner    string `json:"owner,omitempty"`
}

func decode(r io.Reader) (configuration, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 32769))
	if err != nil || len(raw) > 32768 {
		return configuration{}, errors.New("invalid private source configuration")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return configuration{}, errors.New("invalid private source configuration")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil || (key != "address" && key != "scope" && key != "password" && key != "owner") {
			return configuration{}, errors.New("invalid private source configuration")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return configuration{}, errors.New("invalid private source configuration")
		}
		fields[key] = value
	}
	if _, err := d.Token(); err != nil {
		return configuration{}, errors.New("invalid private source configuration")
	}
	if _, err := d.Token(); err != io.EOF {
		return configuration{}, errors.New("invalid private source configuration")
	}
	data, _ := json.Marshal(fields)
	var config configuration
	strict := json.NewDecoder(strings.NewReader(string(data)))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&config); err != nil || (config.Scope != "infra" && config.Scope != "business") ||
		!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(config.Password) {
		return configuration{}, errors.New("invalid private source configuration")
	}
	return config, nil
}

func plan(config configuration, w io.Writer) error {
	if config.Address != "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(config.Owner) {
		return errors.New("invalid private account declaration")
	}
	hash := sha256.Sum256([]byte(config.Password))
	enabled, err := redissource.ObserverACL(config.Scope, hex.EncodeToString(hash[:]), true)
	if err != nil {
		return err
	}
	disabled, _ := redissource.ObserverACL(config.Scope, "", false)
	record := func(args []string) string {
		return "addp.redis-observation/v1:" + config.Scope + ":" + config.Owner + "\nuser " + args[2] + " " + strings.Join(args[3:], " ") + "\n"
	}
	return json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": enabled, "disabled": disabled, "enabled_record": record(enabled), "disabled_record": record(disabled),
		"password_hash": hex.EncodeToString(hash[:]),
	})
}

func serve(config configuration) error {
	address, err := netip.ParseAddrPort(config.Address)
	if err != nil || config.Owner != "" {
		return errors.New("invalid private source address")
	}
	key, err := tls.LoadX509KeyPair("/source/server.crt", "/source/server.key")
	if err != nil {
		return errors.New("source certificate unavailable")
	}
	ca, err := os.ReadFile("/source/ca.crt")
	if err != nil {
		return errors.New("source client trust unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return errors.New("source client trust rejected")
	}
	target := redissource.Target{Address: address, Scope: config.Scope, Password: config.Password}
	server := &http.Server{
		Addr: ":9443", Handler: redissource.Handler(func(ctx context.Context) (redissource.Snapshot, error) { return redissource.Read(ctx, target) }),
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0),
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{key}},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		deadline, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(deadline)
	}()
	err = server.ListenAndServeTLS("", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("source listener unavailable")
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "account-plan" {
		config, err := decode(os.Stdin)
		if err != nil {
			return err
		}
		return plan(config, os.Stdout)
	}
	if len(args) != 0 {
		return errors.New("unexpected source arguments")
	}
	file, err := os.Open("/source/config.json")
	if err != nil {
		return errors.New("private source configuration unavailable")
	}
	defer file.Close()
	config, err := decode(file)
	if err != nil {
		return err
	}
	return serve(config)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		// Never forward raw Redis, TLS, JSON or filesystem failures.
		_, _ = io.WriteString(os.Stderr, "Redis observation source unavailable\n")
		os.Exit(1)
	}
}
