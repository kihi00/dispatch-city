package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/teko/food-delivery/internal/cluster"
	"github.com/teko/food-delivery/internal/simulation"
)

// allowedAdminDeployments is an allowlist so the dashboard can't target
// arbitrary workloads via the admin endpoints.
var allowedAdminDeployments = map[string]bool{
	"control-api":       true,
	"order-worker":      true,
	"restaurant-pizza":  true,
	"restaurant-bowl":   true,
	"restaurant-curry":  true,
}

// Server exposes the simulation over REST and Server-Sent Events.
type Server struct {
	engine   *simulation.Engine
	commands Commands
	logger   *slog.Logger
	http     *http.Server
	cluster  *cluster.Observer // nil outside the Kubernetes cluster -> admin routes return 503
}

// NewServer constructs an API server with all routes registered.
// controller may be nil (e.g. standalone/local dev) — admin endpoints
// then respond with 503 instead of panicking.
func NewServer(addr string, engine *simulation.Engine, commands Commands, logger *slog.Logger, controller *cluster.Observer) *Server {
	mux := http.NewServeMux()
	server := &Server{engine: engine, commands: commands, logger: logger, cluster: controller}
	mux.HandleFunc("GET /api/v1/snapshot", server.snapshot)
	mux.HandleFunc("GET /api/v1/events", server.events)
	mux.HandleFunc("POST /api/v1/simulation/start", server.start)
	mux.HandleFunc("POST /api/v1/simulation/pause", server.pause)
	mux.HandleFunc("POST /api/v1/simulation/reset", server.reset)
	mux.HandleFunc("POST /api/v1/orders", server.createOrder)
	mux.HandleFunc("GET /health/live", server.health)
	mux.HandleFunc("GET /health/ready", server.health)
	mux.HandleFunc("GET /metrics", server.metrics)

	// Admin / cluster-control endpoints (Block 7 Bonus)
	mux.HandleFunc("GET /api/v1/hpa", server.hpaStatus)
	mux.HandleFunc("GET /api/v1/pods/{deployment}", server.podInstances)
	mux.HandleFunc("POST /api/v1/pods/{deployment}/restart", server.restartDeployment)
	mux.HandleFunc("POST /api/v1/pods/{deployment}/scale", server.scaleDeployment)
	mux.HandleFunc("POST /api/v1/pods/{deployment}/chaos", server.chaosKillPod)

	server.http = &http.Server{
		Addr:              addr,
		Handler:           requestLog(logger, cors(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server
}

// ListenAndServe starts accepting HTTP requests.
func (s *Server) ListenAndServe() error {
	if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return nil
}

func (s *Server) snapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Snapshot())
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, unsubscribe := s.engine.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			raw, err := json.Marshal(event)
			if err != nil {
				s.logger.Error("marshal SSE event", "error", err)
				continue
			}
			fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.ID, raw)
			flusher.Flush()
		}
	}
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if err := s.commands.Start(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"running": true})
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	if err := s.commands.Pause(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"running": false})
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	if err := s.commands.Reset(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	order, err := s.commands.CreateOrder(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.engine.Snapshot()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP food_delivery_active_orders Current active orders.\n")
	fmt.Fprintf(w, "# TYPE food_delivery_active_orders gauge\n")
	fmt.Fprintf(w, "food_delivery_active_orders %d\n", snapshot.Stats.ActiveOrders)
	fmt.Fprintf(w, "# HELP food_delivery_delivered_orders_total Delivered orders.\n")
	fmt.Fprintf(w, "# TYPE food_delivery_delivered_orders_total counter\n")
	fmt.Fprintf(w, "food_delivery_delivered_orders_total %d\n", snapshot.Stats.Delivered)
	fmt.Fprintf(w, "# HELP food_delivery_events_total Emitted domain events.\n")
	fmt.Fprintf(w, "# TYPE food_delivery_events_total counter\n")
	fmt.Fprintf(w, "food_delivery_events_total %d\n", snapshot.Stats.Events)
}

// --- Admin / cluster-control (Block 7 Bonus) --------------------------------

func (s *Server) hpaStatus(w http.ResponseWriter, r *http.Request) {
	if s.cluster == nil {
		http.Error(w, "cluster controller not available in this environment", http.StatusServiceUnavailable)
		return
	}
	statuses, err := s.cluster.HorizontalPodAutoscalers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) podInstances(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("deployment")
	if !allowedAdminDeployments[name] {
		http.Error(w, "deployment not allowed: "+name, http.StatusForbidden)
		return
	}
	if s.cluster == nil {
		http.Error(w, "cluster controller not available in this environment", http.StatusServiceUnavailable)
		return
	}
	status, err := s.cluster.PodInstances(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) restartDeployment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("deployment")
	if !allowedAdminDeployments[name] {
		http.Error(w, "deployment not allowed: "+name, http.StatusForbidden)
		return
	}
	if s.cluster == nil {
		http.Error(w, "cluster controller not available in this environment", http.StatusServiceUnavailable)
		return
	}
	if err := s.cluster.RestartDeployment(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restart triggered", "deployment": name})
}

type scaleRequest struct {
	Replicas int `json:"replicas"`
}

func (s *Server) scaleDeployment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("deployment")
	if !allowedAdminDeployments[name] {
		http.Error(w, "deployment not allowed: "+name, http.StatusForbidden)
		return
	}
	if s.cluster == nil {
		http.Error(w, "cluster controller not available in this environment", http.StatusServiceUnavailable)
		return
	}
	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Replicas < 0 || req.Replicas > 5 {
		http.Error(w, "replicas must be 0-5", http.StatusBadRequest)
		return
	}
	if err := s.cluster.ScaleDeployment(r.Context(), name, req.Replicas); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "scaled", "deployment": name, "replicas": req.Replicas})
}

func (s *Server) chaosKillPod(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("deployment")
	if !allowedAdminDeployments[name] {
		http.Error(w, "deployment not allowed: "+name, http.StatusForbidden)
		return
	}
	if s.cluster == nil {
		http.Error(w, "cluster controller not available in this environment", http.StatusServiceUnavailable)
		return
	}
	killed, err := s.cluster.ChaosKillPod(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "chaos: pod gelöscht", "deployment": name, "pod": killed})
}

// --- Helpers -----------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/health/live" && r.URL.Path != "/health/ready" {
			logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", strconv.FormatInt(time.Since(started).Milliseconds(), 10))
		}
	})
}
