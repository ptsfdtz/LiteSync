package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"litesync/server/internal/folderpicker"
	"litesync/server/internal/model"
	"litesync/server/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(svc *service.Service) http.Handler {
	handler := &Handler{
		service: svc,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", handler.handleHealth)
	mux.HandleFunc("/api/config", handler.handleConfig)
	mux.HandleFunc("/api/status", handler.handleStatus)
	mux.HandleFunc("/api/logs", handler.handleLogs)
	mux.HandleFunc("/api/backup", handler.handleBackup)
	mux.HandleFunc("/api/folder-picker", handler.handleFolderPicker)

	return withCORS(mux)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.service.GetConfig())
	case http.MethodPut:
		var cfg model.Config
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := h.service.UpdateConfig(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, h.service.GetConfig())
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config": h.service.GetConfig(),
		"status": h.service.GetStatus(),
	})
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	limit := 100
	rawLimit := r.URL.Query().Get("limit")
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a number")
			return
		}
		limit = parsedLimit
	}

	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logs": h.service.GetLogs(limit),
	})
}

func (h *Handler) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	err := h.service.TriggerBackup(r.Context(), "manual")
	if err != nil {
		if errors.Is(err, service.ErrBackupAlreadyRunning) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *Handler) handleFolderPicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var request struct {
		InitialPath string `json:"initialPath"`
	}

	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	selectedPath, err := folderpicker.Pick(request.InitialPath)
	if err != nil {
		if errors.Is(err, folderpicker.ErrCancelled) {
			writeJSON(w, http.StatusOK, map[string]any{
				"cancelled": true,
				"path":      "",
			})
			return
		}

		if errors.Is(err, folderpicker.ErrNotSupported) {
			writeError(w, http.StatusNotImplemented, "folder picker is only available on Windows desktop mode")
			return
		}

		writeError(w, http.StatusInternalServerError, "failed to open folder picker")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cancelled": false,
		"path":      selectedPath,
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}
