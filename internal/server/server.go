// Package server provides HTTP server setup with chi router and lifecycle management.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/max-marek-projects/avatars-service/internal/handlers"
	"github.com/max-marek-projects/avatars-service/internal/middlewares"
)

// server wraps an http.server with a chi router and the avatar HTTP handler.
type server struct {
	httpSrv *http.Server
	router  chi.Router
	Addr    string
	handler *handlers.Handler
	logger  *slog.Logger
}

// NewServer creates a new HTTP server with all routes registered.
//
// Parameters:
//   - addr: listening address (e.g., ":8080").
//   - h: HTTP handler with business-logic methods.
//   - readTimeout, writeTimeout: server-side I/O deadlines.
//   - staticDir: path to SPA assets; empty disables static serving.
//   - logger: structured logger.
//
// Returns:
//   - *Server: the initialized server instance.
//   - error: non-nil if the handler is nil.
func NewServer(
	addr string,
	h *handlers.Handler,
	readTimeout, writeTimeout time.Duration,
	staticDir string,
	logger *slog.Logger,
) (*server, error) {
	if h == nil {
		return nil, fmt.Errorf("nil handler")
	}
	if logger == nil {
		logger = slog.Default()
	}
	router := NewRouter(h, staticDir, logger)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}
	return &server{
		httpSrv: httpSrv,
		router:  router,
		Addr:    addr,
		handler: h,
		logger:  logger,
	}, nil
}

// Router returns the underlying chi router. Useful for tests and for mounting
// the server under a larger application.
func (s *server) Router() chi.Router {
	return s.router
}

// NewRouter builds and returns the fully-wired chi.Router.
// staticDir may be empty — in that case static assets and the SPA
// entry point are not registered. logger may be nil.
func NewRouter(h *handlers.Handler, staticDir string, logger *slog.Logger) chi.Router {
	if logger == nil {
		logger = slog.Default()
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middlewares.RequestsLogger(logger))

	r.Get("/health", h.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/avatars", func(r chi.Router) {
			r.Post("/", h.UploadAvatar)
			r.Get("/{avatar_id}", h.GetAvatarByID)
			r.Delete("/{avatar_id}", h.DeleteAvatar)
			r.Get("/{avatar_id}/metadata", h.GetAvatarMetadata)
		})
		r.Route("/users/{user_id}", func(r chi.Router) {
			r.Get("/avatar", h.GetActiveAvatar)
			r.Delete("/avatar", h.DeleteActiveUserAvatar)
			r.Get("/avatars", h.ListUserAvatars)
		})
	})

	r.Route("/web", func(r chi.Router) {
		r.Get("/upload", h.WebUploadForm)
		r.Post("/upload", h.WebUpload)
		r.Get("/gallery/{user_id}", h.WebGallery)
	})

	if staticDir != "" {
		mountStatic(r, staticDir, logger)
	}

	return r
}

func mountStatic(r chi.Router, staticDir string, log *slog.Logger) {
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		log.Warn("static dir missing index.html, SPA disabled",
			slog.String("dir", staticDir), slog.Any("error", err))
		return
	}

	fs := http.FileServer(http.Dir(staticDir))

	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, filepath.Join(staticDir, "index.html"))
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.NotFound(w, req)
			return
		}
		http.ServeFile(w, req, filepath.Join(staticDir, "index.html"))
	})

	log.Info("static assets mounted", slog.String("dir", staticDir))
}

// ListenAndServe starts the HTTP server on the configured address.
// Blocks until the server is stopped or fails.
func (s *server) ListenAndServe() error {
	s.logger.Info("Starting HTTP server", slog.String("address", s.Addr))
	if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("http server stopped", slog.Any("error", err))
		return fmt.Errorf("server run error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server and closes the handler.
// ctx bounds the grace period for in-flight requests.
func (s *server) Shutdown(ctx context.Context) error {
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	return s.handler.Close(ctx)
}
