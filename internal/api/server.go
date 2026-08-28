package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	logger    *slog.Logger
	readiness func(context.Context) error
	apiKey    string
	endpoints EndpointService
	events    EventService
	startedAt time.Time
}

func New(logger *slog.Logger, ready func(context.Context) error, apiKey string, endpoints EndpointService, events EventService) *Server {
	return &Server{
		logger:    logger,
		readiness: ready,
		apiKey:    apiKey,
		endpoints: endpoints,
		events:    events,
		startedAt: time.Now().UTC(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)

	private := http.NewServeMux()
	private.HandleFunc("POST /v1/endpoints", s.createEndpoint)
	private.HandleFunc("GET /v1/endpoints", s.listEndpoints)
	private.HandleFunc("GET /v1/endpoints/{endpoint_id}", s.getEndpoint)
	private.HandleFunc("DELETE /v1/endpoints/{endpoint_id}", s.disableEndpoint)
	private.HandleFunc("POST /v1/events", s.publishEvent)
	mux.Handle("/v1/", s.authenticate(private))
	return s.accessLog(mux)
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"started_at": s.startedAt,
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	if err := s.readiness(ctx); err != nil {
		s.logger.Warn("readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(response, r)
		s.logger.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", response.status,
			"bytes", response.bytes,
			"duration", time.Since(started),
		)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		authorized := found && scheme == "Bearer" && subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) == 1
		if !authorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(body)
	w.bytes += written
	return written, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
