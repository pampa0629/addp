package dbbridge

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestOpenScriptSessionUsesRegisteredProvider(t *testing.T) {
	engine := &models.Engine{
		ID: 10, Name: "Jupyter Engine", EngineType: "jupyter",
		ConnectionInfo: models.ConnectionInfo{
			"protocol": "http", "host": "127.0.0.1", "port": 8097,
		},
	}
	session, err := OpenScriptSession(context.Background(), engine, plugin.ScriptSessionRequest{
		Mode: "notebook", Language: "python",
	})
	if err != nil {
		t.Fatalf("OpenScriptSession() error = %v", err)
	}
	if session.Endpoint != "http://127.0.0.1:8097" {
		t.Fatalf("Endpoint = %q", session.Endpoint)
	}
	if session.Info["mode"] != "notebook" || session.Info["language"] != "python" {
		t.Fatalf("Info = %#v", session.Info)
	}
}

func TestOpenScriptSessionRejectsNonScriptProvider(t *testing.T) {
	_, err := OpenScriptSession(context.Background(), &models.Engine{
		EngineType: "postgresql", ConnectionInfo: models.ConnectionInfo{},
	}, plugin.ScriptSessionRequest{Mode: "notebook"})
	if err == nil {
		t.Fatal("expected non-script provider to be rejected")
	}
}
