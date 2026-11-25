package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlyOneSuccessfulMatchingResponseIsHeld(t *testing.T) {
	dir := t.TempDir()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusForbidden)
		}
		fmt.Fprint(w, "original")
	}))
	defer up.Close()
	dest, _ := url.Parse(up.URL)
	server := httptest.NewServer(forwardMeta(dest, dir))
	defer server.Close()
	defer os.WriteFile(filepath.Join(dir, "released"), nil, 0600)
	if err := os.WriteFile(filepath.Join(dir, "armed"), []byte("1\npublic.sample"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/meta/items/by-catalog-path?catalog_path=public.sample&engine_id="
	get := func(id, token string) string {
		req, _ := http.NewRequest("GET", server.URL+path+id, nil)
		req.Header.Set("Authorization", token)
		res, err := server.Client().Do(req)
		if err != nil {
			return err.Error()
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return fmt.Sprintf("%d %s", res.StatusCode, body)
	}
	if get("1", "") != "403 original" || get("2", "Bearer fixture") != "200 original" {
		t.Fatal("unrelated response changed")
	}
	done := make(chan string, 1)
	go func() { done <- get("1", "Bearer fixture") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "held")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("response was not held")
		}
		time.Sleep(time.Millisecond)
	}
	if get("1", "Bearer fixture") != "200 original" {
		t.Fatal("second response changed")
	}
	select {
	case <-done:
		t.Fatal("first response returned before release")
	default:
	}
	if err := os.WriteFile(filepath.Join(dir, "released"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result != "200 original" {
		t.Fatal(result)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "forwarded")); err != nil || string(data) != "1\npublic.sample" {
		t.Fatal("missing exact source receipt")
	}
}
