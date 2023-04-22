package server

import (
	"fmt"

	"github.com/go-zoox/zoox/defaults"
)

type Server interface {
	Run(cfg *Config) error
}

type Config struct {
	Port        int64
	Shell       string
	Context     string
	Environment map[string]string
	Timeout     int64
	Username    string
	Password    string
}

type server struct {
}

func NewServer() Server {
	return &server{}
}

func Serve(cfg *Config) error {
	s := NewServer()
	return s.Run(cfg)
}

func (s *server) Run(cfg *Config) error {
	app := defaults.Application()

	if cfg.Username != "" && cfg.Password != "" {
		app.Use(createAuthMiddleware(cfg))
	}

	app.WebSocket("/ws", createWsService(cfg))

	return app.Run(fmt.Sprintf("0.0.0.0:%d", cfg.Port))
}
