package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerScopedRegistration(t *testing.T) {
	h := routes(testStore(t))
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"name":"Guild"}`, 400},
		{`{"name":"Guild","server":" "}`, 400},
		{`{"name":"Guild","server":null}`, 400},
		{`{"name":"Guild","server":12}`, 400},
		{`{"name":"Guild","server":"a\nb"}`, 400},
		{`{"name":"Guild","server":"` + strings.Repeat("x", 101) + `"}`, 400},
		{`{"name":"Guild","server":" Server   One "}`, 201},
		{`{"name":"guild","server":"server one"}`, 409},
		{`{"name":"Guild","server":"Server Two"}`, 201},
	} {
		w := requestGuild(h, "POST", "/api/v1/guilds", tc.body, "")
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d: %s", tc.body, w.Code, tc.status, w.Body.String())
		}
	}
}

func TestVersionOneMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sagetracker.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	key := "sgt_" + strings.Repeat("x", 43)
	hash := sha256.Sum256([]byte(key))
	_, err = db.Exec(`CREATE TABLE guilds (id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE, key_hash BLOB NOT NULL UNIQUE, created_at TEXT NOT NULL); PRAGMA user_version = 1;`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO guilds VALUES ('legacy-id','Guild','guild',?,'2026-09-14T00:00:00Z')`, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.db.Close() }()
	g, err := s.guildForKey(context.Background(), key)
	if err != nil || g.ID != "legacy-id" || g.Server != "" || g.CreatedAt != "2026-09-14T00:00:00Z" {
		t.Fatalf("migration failed: %v %v", g, err)
	}
	h := routes(s)
	if w := requestGuild(h, "PATCH", "/api/v1/guilds/me/server", `{"server":"Server One"}`, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := requestGuild(h, "PATCH", "/api/v1/guilds/me/server", `{}`, key); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if _, _, err := s.createGuild(context.Background(), "Guild", "Taken"); err != nil {
		t.Fatal(err)
	}
	if w := requestGuild(h, "PATCH", "/api/v1/guilds/me/server", `{"server":"Taken"}`, key); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := requestGuild(h, "PATCH", "/api/v1/guilds/me/server", `{"server":" Server   One "}`, key); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := requestGuild(h, "PATCH", "/api/v1/guilds/me/server", `{"server":"Server Two"}`, key); w.Code != 409 {
		t.Fatal(w.Code)
	}
	s.db.Close()
	s, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.guildForKey(context.Background(), key)
	if err != nil || g.Server != "Server One" || g.ID != "legacy-id" {
		t.Fatalf("assignment did not persist: %v %v", g, err)
	}
	if w := requestGuild(routes(s), "POST", "/api/v1/guilds", `{"name":"Guild","server":"server one"}`, ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
}
