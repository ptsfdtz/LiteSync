package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"litesync/server/internal/httpapi"
	"litesync/server/internal/service"
)

func main() {
	addr := envOrDefault("LITESYNC_HTTP_ADDR", ":8080")
	webDir := envOrDefault("LITESYNC_WEB_DIR", "./web")

	dataDir, err := resolveDataDir()
	if err != nil {
		log.Fatalf("failed to resolve data directory: %v", err)
	}

	svc, err := service.New(dataDir)
	if err != nil {
		log.Fatalf("failed to initialize service: %v", err)
	}
	defer svc.Shutdown()

	apiHandler := httpapi.New(svc)
	rootHandler := buildRootHandler(apiHandler, webDir)

	server := &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("LiteSync server started on %s", addr)
		log.Printf("Config data directory: %s", dataDir)
		log.Printf("Web assets directory: %s", webDir)
		if listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			log.Fatalf("server stopped unexpectedly: %v", listenErr)
		}
	}()

	waitForShutdownSignal()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}

func resolveDataDir() (string, error) {
	dataDir := strings.TrimSpace(os.Getenv("LITESYNC_DATA_DIR"))
	if dataDir != "" {
		return filepath.Abs(dataDir)
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(userConfigDir, "LiteSync"), nil
}

func buildRootHandler(apiHandler http.Handler, webDir string) http.Handler {
	indexFilePath := filepath.Join(webDir, "index.html")
	indexExists := fileExists(indexFilePath)

	if !indexExists {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAPIRequest(r.URL.Path) {
				apiHandler.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body style="font-family:sans-serif;padding:24px"><h2>LiteSync API is running</h2><p>Web UI assets not found. Build the client and set <code>LITESYNC_WEB_DIR</code>.</p></body></html>`))
		})
	}

	fileServer := http.FileServer(http.Dir(webDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIRequest(r.URL.Path) {
			apiHandler.ServeHTTP(w, r)
			return
		}

		cleanPath := path.Clean(r.URL.Path)
		if cleanPath == "." {
			cleanPath = "/"
		}

		if cleanPath == "/" {
			http.ServeFile(w, r, indexFilePath)
			return
		}

		staticFilePath := filepath.Join(webDir, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
		if info, err := os.Stat(staticFilePath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexFilePath)
	})
}

func isAPIRequest(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func waitForShutdownSignal() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	<-signalChannel
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}
