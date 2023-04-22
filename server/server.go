package server

import (
	"fmt"

	"github.com/go-zoox/zoox/defaults"
)

// Server is the server interface of caas
type Server interface {
	Run() error
}

// Config is the configuration of caas server
type Config struct {
	Port        int64
	Shell       string
	Context     string
	Environment map[string]string
	Timeout     int64
	// Auth
	ClientID     string
	ClientSecret string
	AuthService  string
}

type server struct {
	cfg *Config
}

// New creates a new caas server
func New(cfg *Config) Server {
	return &server{
		cfg: cfg,
	}
}

func (s *server) Run() error {
	app := defaults.Application()

	app.WebSocket("/ws", createWsService(s.cfg))

	return app.Run(fmt.Sprintf("0.0.0.0:%d", s.cfg.Port))
}
