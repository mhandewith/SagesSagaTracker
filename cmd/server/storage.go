package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type store struct{ db *sql.DB }

type guild struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Server    string `json:"server"`
	CreatedAt string `json:"created_at"`
}

var errGuildExists = errors.New("guild name already registered")

func openStore(path string) (*store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection serializes writes and keeps these connection-local pragmas active.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &store{db: db}
	if err := s.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database %s: %w", path, err)
	}
	return s, nil
}

func (s *store) initialize() error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000", "PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL", "PRAGMA synchronous = FULL",
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var schemaVersion int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		return err
	}
	if schemaVersion > 2 {
		return fmt.Errorf("database schema %d is newer than this application supports", schemaVersion)
	}
	if schemaVersion == 0 {
		if _, err := tx.Exec(`CREATE TABLE guilds (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL UNIQUE,
			key_hash BLOB NOT NULL UNIQUE,
			created_at TEXT NOT NULL
		); PRAGMA user_version = 1;`); err != nil {
			return err
		}
	}
	if schemaVersion < 2 {
		if _, err := tx.Exec(`CREATE TABLE guilds_v2 (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL,
			server TEXT, server_key TEXT,
			key_hash BLOB NOT NULL UNIQUE, created_at TEXT NOT NULL,
			UNIQUE(server_key, name_key)
		);
		INSERT INTO guilds_v2 (id, name, name_key, key_hash, created_at)
		SELECT id, name, name_key, key_hash, created_at FROM guilds;
		DROP TABLE guilds;
		ALTER TABLE guilds_v2 RENAME TO guilds;
		PRAGMA user_version = 2;`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *store) createGuild(ctx context.Context, name, server string) (guild, string, error) {
	var secret [32]byte
	var id [16]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return guild{}, "", err
	}
	if _, err := rand.Read(id[:]); err != nil {
		return guild{}, "", err
	}
	key := "sgt_" + base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(key))
	g := guild{ID: hex.EncodeToString(id[:]), Name: name, Server: server, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	result, err := s.db.ExecContext(ctx, `INSERT INTO guilds (id, name, name_key, server, server_key, key_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(server_key, name_key) DO NOTHING`, g.ID, g.Name, strings.ToLower(name), server, strings.ToLower(server), hash[:], g.CreatedAt)
	if err != nil {
		return guild{}, "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return guild{}, "", err
	}
	if n == 0 {
		return guild{}, "", errGuildExists
	}
	return g, key, nil
}

func (s *store) guildForKey(ctx context.Context, key string) (guild, error) {
	hash := sha256.Sum256([]byte(key))
	var g guild
	err := s.db.QueryRowContext(ctx, "SELECT id, name, COALESCE(server, ''), created_at FROM guilds WHERE key_hash = ?", hash[:]).Scan(&g.ID, &g.Name, &g.Server, &g.CreatedAt)
	return g, err
}

var errServerAssigned = errors.New("server is already assigned or name is taken on that server")

func (s *store) assignServer(ctx context.Context, id, server string) error {
	// OR IGNORE handles a name collision without replacing either guild.
	result, err := s.db.ExecContext(ctx, `UPDATE OR IGNORE guilds SET server = ?, server_key = ? WHERE id = ? AND server IS NULL`, server, strings.ToLower(server), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errServerAssigned
	}
	return nil
}

func (s *store) ready(ctx context.Context) error {
	var n int
	return s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM guilds)").Scan(&n)
}
