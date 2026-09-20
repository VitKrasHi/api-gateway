package server

import (
	"context"
	"net/http"

	"api-gateway/internal/config"
)

type Server struct {
	srv *http.Server
}

func New(cfg *config.Config, handler http.Handler) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              ":" + cfg.Port,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
	}
}

func (s *Server) Addr() string                       { return s.srv.Addr }
func (s *Server) ListenAndServe() error              { return s.srv.ListenAndServe() }
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }
