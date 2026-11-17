package bootstrap_test

import (
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/redistest"
	"testing"
)

func TestRequiredRedisAgainstRedis(t *testing.T) {
	cfg := redistest.Config(t)
	client := redistest.Open(t, cfg)
	if err := client.Ping(t.Context()).Err(); err != nil {
		t.Fatal(err)
	}
	cfg.Password += "-wrong"
	if client, err := bootstrap.OpenRequiredRedis(t.Context(), cfg); err == nil || client != nil {
		t.Fatal("wrong credential returned a usable client")
	}
}
