package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

func registerGuildRoutes(mux *http.ServeMux, storage *store) {
	mux.HandleFunc("POST /api/v1/guilds", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if contentType != "application/json" {
			apiError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input struct {
			Name   string `json:"name"`
			Server string `json:"server"`
		}
		if err := decoder.Decode(&input); err != nil {
			invalidBody(w, err)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			invalidBody(w, err)
			return
		}
		for _, ch := range input.Name {
			if unicode.IsControl(ch) {
				apiError(w, http.StatusBadRequest, "name must not contain control characters")
				return
			}
		}
		name := strings.Join(strings.Fields(input.Name), " ")
		if name == "" || utf8.RuneCountInString(name) > 100 {
			apiError(w, http.StatusBadRequest, "name must contain 1 to 100 characters")
			return
		}
		server, ok := validateServer(w, input.Server)
		if !ok {
			return
		}
		g, key, err := storage.createGuild(r.Context(), name, server)
		if errors.Is(err, errGuildExists) {
			apiError(w, http.StatusConflict, "a guild with that name already exists on that server")
			return
		}
		if err != nil {
			slog.Error("register guild", "error", err)
			apiError(w, http.StatusInternalServerError, "unable to register guild")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, struct {
			Guild guild  `json:"guild"`
			Key   string `json:"key"`
		}{g, key})
	})
	mux.HandleFunc("GET /api/v1/guilds/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 47 || !strings.HasPrefix(parts[1], "sgt_") {
			unauthorized(w)
			return
		}
		g, err := storage.guildForKey(r.Context(), parts[1])
		if errors.Is(err, sql.ErrNoRows) {
			unauthorized(w)
			return
		}
		if err != nil {
			slog.Error("validate guild key", "error", err)
			apiError(w, http.StatusInternalServerError, "unable to validate guild key")
			return
		}
		writeJSON(w, struct {
			Guild guild `json:"guild"`
		}{g})
	})
	mux.HandleFunc("PATCH /api/v1/guilds/me/server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			unauthorized(w)
			return
		}
		g, err := storage.guildForKey(r.Context(), parts[1])
		if errors.Is(err, sql.ErrNoRows) {
			unauthorized(w)
			return
		}
		if err != nil {
			apiError(w, 500, "unable to validate guild key")
			return
		}
		contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if contentType != "application/json" {
			apiError(w, 415, "Content-Type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input struct {
			Server string `json:"server"`
		}
		if err := decoder.Decode(&input); err != nil {
			invalidBody(w, err)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			invalidBody(w, err)
			return
		}
		server, ok := validateServer(w, input.Server)
		if !ok {
			return
		}
		if err := storage.assignServer(r.Context(), g.ID, server); err != nil {
			if errors.Is(err, errServerAssigned) {
				apiError(w, 409, err.Error())
				return
			}
			apiError(w, 500, "unable to assign server")
			return
		}
		g.Server = server
		writeJSON(w, struct {
			Guild guild `json:"guild"`
		}{g})
	})
}

func validateServer(w http.ResponseWriter, value string) (string, bool) {
	for _, ch := range value {
		if unicode.IsControl(ch) {
			apiError(w, 400, "server must not contain control characters")
			return "", false
		}
	}
	value = strings.Join(strings.Fields(value), " ")
	if value == "" || utf8.RuneCountInString(value) > 100 {
		apiError(w, 400, "server must contain 1 to 100 characters")
		return "", false
	}
	return value, true
}

func invalidBody(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apiError(w, http.StatusRequestEntityTooLarge, "request body exceeds 4096 bytes")
		return
	}
	apiError(w, http.StatusBadRequest, "body must be one JSON object containing only the supported fields")
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	apiError(w, http.StatusUnauthorized, "missing or invalid guild key")
}

func apiError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}
