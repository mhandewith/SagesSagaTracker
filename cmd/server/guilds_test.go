package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func requestGuild(h http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGuildRegistrationPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sagetracker.db")
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.db.Close() }()
	w := requestGuild(routes(s), "POST", "/api/v1/guilds", `{"name":"  Test   Guild  "}`, "")
	if w.Code != 201 {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	var registration struct {
		Guild guild  `json:"guild"`
		Key   string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	if registration.Guild.Name != "Test Guild" || registration.Guild.ID == "" || len(registration.Key) != 47 {
		t.Fatal("invalid registration response")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("key response must not be cached")
	}
	var stored []byte
	if err := s.db.QueryRow("SELECT key_hash FROM guilds").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(registration.Key))
	if string(stored) != string(expected[:]) {
		t.Fatal("expected hashed key")
	}
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	w = requestGuild(routes(s), "GET", "/api/v1/guilds/me", "", registration.Key)
	if w.Code != 200 {
		t.Fatalf("persisted key: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), registration.Key) || strings.Contains(w.Body.String(), "key_hash") {
		t.Fatal("key leaked in validation response")
	}
	var validated struct {
		Guild guild `json:"guild"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &validated); err != nil {
		t.Fatal(err)
	}
	if validated.Guild != registration.Guild {
		t.Fatal("guild changed after reopen")
	}
	w = requestGuild(routes(s), "POST", "/api/v1/guilds", `{"name":"test guild"}`, "")
	if w.Code != 409 {
		t.Fatalf("duplicate name: %d", w.Code)
	}
}

func TestInvalidGuildRequests(t *testing.T) {
	h := routes(testStore(t))
	for _, body := range []string{`{`, `{}`, `null`, `{"name":" "}`, `{"name":12}`, `{"name":"a","extra":true}`, `{"name":"a"}{}`, `{"name":"a\nb"}`, `{"name":"` + strings.Repeat("a", 101) + `"}`} {
		w := requestGuild(h, "POST", "/api/v1/guilds", body, "")
		if w.Code != 400 {
			t.Fatalf("body %q: %d", body, w.Code)
		}
	}
	w := requestGuild(h, "POST", "/api/v1/guilds", `{"name":"`+strings.Repeat("a", 5000)+`"}`, "")
	if w.Code != 413 {
		t.Fatalf("oversized request: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/v1/guilds", strings.NewReader(`{"name":"a"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatalf("missing content type: %d", w.Code)
	}
	for _, key := range []string{"", "bad", "sgt_" + strings.Repeat("a", 43)} {
		w = requestGuild(h, "GET", "/api/v1/guilds/me", "", key)
		if w.Code != 401 {
			t.Fatalf("invalid key: %d", w.Code)
		}
	}
}

func TestConcurrentDuplicateRegistration(t *testing.T) {
	h := routes(testStore(t))
	statuses := make(chan int, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- requestGuild(h, "POST", "/api/v1/guilds", `{"name":"Concurrent"}`, "").Code
		}()
	}
	wg.Wait()
	close(statuses)
	created := 0
	for status := range statuses {
		if status == 201 {
			created++
		} else if status != 409 {
			t.Fatalf("unexpected status: %d", status)
		}
	}
	if created != 1 {
		t.Fatalf("created %d guilds", created)
	}
}

func TestDatabaseFailures(t *testing.T) {
	s := testStore(t)
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := s.initialize(); err == nil {
		t.Fatal("newer schema must be rejected")
	}
	s.db.Close()
	w := requestGuild(routes(s), "GET", "/healthz", "", "")
	if w.Code != 503 {
		t.Fatalf("closed DB health: %d", w.Code)
	}
}

func TestKeysAreDistinctAndScoped(t *testing.T) {
	s := testStore(t)
	a, ka, err := s.createGuild(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}
	b, kb, err := s.createGuild(context.Background(), "B")
	if err != nil {
		t.Fatal(err)
	}
	if ka == kb || a.ID == b.ID {
		t.Fatal("guild identities must be distinct")
	}
	for key, expected := range map[string]guild{ka: a, kb: b} {
		actual, err := s.guildForKey(context.Background(), key)
		if err != nil || actual != expected {
			t.Fatal("key resolved to wrong guild")
		}
	}
}
