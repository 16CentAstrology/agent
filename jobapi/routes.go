package jobapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/exp/maps"
)

// router returns a chi router with the jobapi routes and appropriate middlewares mounted
func (s *Server) router() chi.Router {
	middlewares := [](func(http.Handler) http.Handler){}
	if s.debug {
		middlewares = append(middlewares, socket.LoggerMiddleware("Job API", s.Logger.Commentf))
	}
	middlewares = append(middlewares,
		middleware.Recoverer,

		// All responses are in JSON.
		socket.HeadersMiddleware(http.Header{"Content-Type": []string{"application/json"}}),
		socket.AuthMiddleware(s.token, s.Logger.Errorf),
	)

	r := chi.NewRouter()
	r.Use(middlewares...)

	r.Route("/api/current-job/v0", func(r chi.Router) {
		r.Get("/env", s.getEnv)
		r.Patch("/env", s.patchEnv)
		r.Delete("/env", s.deleteEnv)
	})

	return r
}

func (s *Server) getEnv(w http.ResponseWriter, _ *http.Request) {
	s.mtx.RLock()
	normalizedEnv := s.environ.Dump()
	s.mtx.RUnlock()

	resp := EnvGetResponse{Env: normalizedEnv}
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func (s *Server) patchEnv(w http.ResponseWriter, r *http.Request) {
	var req EnvUpdateRequestPayload
	err := json.NewDecoder(r.Body).Decode(&req)
	defer r.Body.Close()
	if err != nil {
		if err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest); err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	added := make([]string, 0, len(req.Env))
	updated := make([]string, 0, len(req.Env))
	protected := checkProtected(maps.Keys(req.Env))

	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("the following environment variables are protected, and cannot be modified: % v", protected),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	nils := make([]string, 0, len(req.Env))

	for k, v := range req.Env {
		if v == nil {
			nils = append(nils, k)
		}
	}

	if len(nils) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("removing environment variables (ie setting them to null) is not permitted on this endpoint. The following keys were set to null: % v", nils),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	for k, v := range req.Env {
		if _, ok := s.environ.Get(k); ok {
			updated = append(updated, k)
		} else {
			added = append(added, k)
		}
		s.environ.Set(k, *v)
	}
	s.mtx.Unlock()

	resp := EnvUpdateResponse{
		Added:   added,
		Updated: updated,
	}

	resp.Normalize()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	var req EnvDeleteRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	defer r.Body.Close()
	if err != nil {
		err := socket.WriteError(w, fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	protected := checkProtected(req.Keys)
	if len(protected) > 0 {
		err := socket.WriteError(
			w,
			fmt.Sprintf("the following environment variables are protected, and cannot be modified: % v", protected),
			http.StatusUnprocessableEntity,
		)
		if err != nil {
			s.Logger.Errorf("Job API: couldn't write error: %v", err)
		}
		return
	}

	s.mtx.Lock()
	deleted := make([]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		if _, ok := s.environ.Get(k); ok {
			deleted = append(deleted, k)
			s.environ.Remove(k)
		}
	}
	s.mtx.Unlock()

	resp := EnvDeleteResponse{Deleted: deleted}
	resp.Normalize()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Errorf("Job API: couldn't encode or write response: %v", err)
	}
}

func checkProtected(candidates []string) []string {
	protected := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := agent.ProtectedEnv[c]; ok {
			protected = append(protected, c)
		}
	}
	return protected
}
