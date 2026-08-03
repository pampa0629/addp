package service

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestNotebookSessionResolveRequiresMatchingSecretAndLiveSession(t *testing.T) {
	secret := "browser-capability"
	service := &NotebookSessionService{items: map[string]*NotebookSession{
		"live": {
			ID: "live", TaskID: 12, TenantID: 7, UserID: 9,
			Endpoint: "http://jupyter:31000", RuntimeToken: "runtime-secret",
			ExpiresAt: time.Now().Add(time.Minute), secretHash: sha256.Sum256([]byte(secret)),
		},
	}}
	resolved, err := service.Resolve("live", secret)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.RuntimeToken != "runtime-secret" {
		t.Fatalf("resolved session = %#v", resolved)
	}
	if _, err := service.Resolve("live", "wrong"); err != ErrNotebookSessionNotFound {
		t.Fatalf("wrong secret error = %v", err)
	}
	service.items["live"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := service.Resolve("live", secret); err != ErrNotebookSessionNotFound {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestNotebookSessionResolveKernelCapabilityRequiresBoundLiveToken(t *testing.T) {
	token := "addp_nkc_kernel-capability"
	service := &NotebookSessionService{items: map[string]*NotebookSession{
		"live": {
			ID: "live", TaskID: 12, TenantID: 7, UserID: 9,
			ExpiresAt: time.Now().Add(time.Minute), kernelCapabilityHash: sha256.Sum256([]byte(token)),
		},
	}}

	resolved, err := service.ResolveKernelCapability("live", token)
	if err != nil {
		t.Fatalf("ResolveKernelCapability() error = %v", err)
	}
	if resolved.TenantID != 7 || resolved.UserID != 9 || resolved.TaskID != 12 {
		t.Fatalf("resolved session = %#v", resolved)
	}
	for _, invalid := range []string{"kernel-capability", "addp_nkc_wrong", ""} {
		if _, err := service.ResolveKernelCapability("live", invalid); err != ErrNotebookSessionNotFound {
			t.Fatalf("token %q error = %v", invalid, err)
		}
	}
	service.items["live"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := service.ResolveKernelCapability("live", token); err != ErrNotebookSessionNotFound {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestPublicNotebookSessionDoesNotExposeProxyCredentials(t *testing.T) {
	public := publicNotebookSession(&NotebookSession{
		ID: "session", TaskID: 12, URL: "/proxy", ExpiresAt: time.Now().Add(time.Hour),
		Endpoint: "http://jupyter:31000", RuntimeToken: "runtime-secret", ControlURL: "http://jupyter:8097",
		kernelCapabilityHash: sha256.Sum256([]byte("addp_nkc_kernel-secret")),
	})
	if public.Endpoint != "" || public.RuntimeToken != "" || public.ControlURL != "" {
		t.Fatalf("public session leaked internal facts: %#v", public)
	}
	if public.kernelCapabilityHash != ([32]byte{}) {
		t.Fatal("public session leaked the kernel capability hash")
	}
}
