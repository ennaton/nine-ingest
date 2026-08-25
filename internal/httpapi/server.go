// Package httpapi is the edge: authenticate, validate, publish, return.
//
// Nothing is processed here. An accepted event is written to Kafka and the
// request returns; everything downstream reads the log. That is what keeps
// this service's p99 a function of the broker write and nothing else.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ennaton/nine-ingest/internal/auth"
	"github.com/ennaton/nine-ingest/internal/event"
	"github.com/ennaton/nine-ingest/internal/kafka"
)

const TopicEvents = "events"

type Server struct {
	keys     *auth.Keys
	producer kafka.Producer
	log      *slog.Logger
}

func New(keys *auth.Keys, p kafka.Producer, log *slog.Logger) *Server {
	return &Server{keys: keys, producer: p, log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", s.postEvent)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	return mux
}

// problem is application/problem+json, the same error shape nine-billing uses.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{"about:blank", title, status, detail})
}

type accepted struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
}

func (s *Server) postEvent(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	tenant, err := s.keys.Resolve(r.Header.Get("X-Api-Key"))
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid X-Api-Key")
		return
	}

	ev, err := event.Decode(r.Body)
	if err != nil {
		var ve *event.ValidationError
		detail := "the event does not match agent_run.v1"
		if errors.As(err, &ve) {
			detail = ve.Error()
		}
		// Logged with the field name, never the body: the rejected field is
		// the one most likely to hold something that must not be logged.
		s.log.Info("event rejected", "tenant", tenant, "reason", detail)
		writeProblem(w, http.StatusUnprocessableEntity, "Invalid event", detail)
		return
	}

	// Re-encode from the validated struct rather than forwarding the original
	// bytes. Anything the schema does not know about cannot survive this step,
	// which makes "unknown data never reaches the log" a property of the code
	// path and not of the validator alone.
	payload, err := json.Marshal(ev)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal error", "could not encode the event")
		return
	}

	if err := s.producer.Publish(r.Context(), kafka.Message{
		Topic: TopicEvents,
		Key:   []byte(tenant),
		Value: payload,
	}); err != nil {
		// The client should retry: the event is not lost, it is not accepted.
		s.log.Error("publish failed", "tenant", tenant, "err", err)
		w.Header().Set("Retry-After", "1")
		writeProblem(w, http.StatusServiceUnavailable, "Not accepted", "the event log is unavailable, retry")
		return
	}

	s.log.Info("event accepted", "tenant", tenant, "agent", ev.Agent, "took_ms", time.Since(start).Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(accepted{EventID: ev.EventID, Status: "accepted"})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readyz is separate from healthz on purpose: alive is not the same as able to
// accept events. When the broker check lands, it belongs here and not above.
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}
