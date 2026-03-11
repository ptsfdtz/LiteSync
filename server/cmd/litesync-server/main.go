package main

import (
	"context"
	"errors"
	"io/fs"
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
	"litesync/server/internal/webui"
)

func main() {
	addr := envOrDefault("LITESYNC_HTTP_ADDR", ":8080")
	webDir := strings.TrimSpace(os.Getenv("LITESYNC_WEB_DIR"))

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
	rootHandler, webSource := buildRootHandler(apiHandler, webDir)

	server := &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("LiteSync server started on %s", addr)
		log.Printf("Config data directory: %s", dataDir)
		log.Printf("Web UI source: %s", webSource)
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

func buildRootHandler(apiHandler http.Handler, webDir string) (http.Handler, string) {
	if webDir != "" {
		diskFS := os.DirFS(webDir)
		if hasFile(diskFS, "index.html") {
			return buildSPAHandler(apiHandler, diskFS), "filesystem: " + webDir
		}
		log.Printf("LITESYNC_WEB_DIR is set but index.html not found: %s", webDir)
	}

	embeddedFS, err := webui.DistFS()
	if err == nil && hasFile(embeddedFS, "index.html") {
		return buildSPAHandler(apiHandler, embeddedFS), "embedded assets"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIRequest(r.URL.Path) {
			apiHandler.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body style="font-family:sans-serif;padding:24px"><h2>LiteSync API is running</h2><p>Web UI assets are missing. Build client and embed assets, or set <code>LITESYNC_WEB_DIR</code>.</p></body></html>`))
	}), "none"
}

func buildSPAHandler(apiHandler http.Handler, staticFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))

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
			http.ServeFileFS(w, r, staticFS, "index.html")
			return
		}

		assetPath := strings.TrimPrefix(cleanPath, "/")
		if hasFile(staticFS, assetPath) {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFileFS(w, r, staticFS, "index.html")
	})
}

func isAPIRequest(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
}

func hasFile(fileSystem fs.FS, name string) bool {
	info, err := fs.Stat(fileSystem, name)
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
