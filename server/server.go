package server

import (
	"fmt"
	"os"

	"github.com/go-zoox/fs"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/zoox/defaults"
)

const DefaultShell = "sh"

// Server is the server interface of caas
type Server interface {
	Run() error
}

// Config is the configuration of caas server
type Config struct {
	Port        int64             `config:"port,default=8838"`
	Shell       string            `config:"shell"`
	Environment map[string]string `config:"environment"`
	Timeout     int64             `config:"timeout"`
	// Auth
	ClientID     string `config:"client_id"`
	ClientSecret string `config:"client_secret"`
	AuthService  string `config:"auth_service"`
	//
	MetadataDir string `config:"metadatadir"`
	//
	WorkDir string `config:"workdir"`
	//
	IsAutoCleanUserWorkDir bool `config:"is_auto_clean_user_workdir"`
}

// CommandConfig is the configuration of caas command
type CommandConfig struct {
	WorkDir   string
	Script    *WriterFile
	Log       *WriterFile
	Env       *WriterFile
	StartAt   *WriterFile
	SucceedAt *WriterFile
	FailedAt  *WriterFile
	Status    *WriterFile
	Error     *WriterFile
}

func (c *Config) GetCommandConfig(id string) (*CommandConfig, error) {
	var isNeedWrite bool
	var oneMetadataDir string

	if c.MetadataDir == "" {
		c.MetadataDir = "/tmp/gzcaas/metadata"
	}

	if c.WorkDir == "" {
		c.WorkDir = "/tmp/gzcaas/workdir"
	}

	oneMetadataDir = fmt.Sprintf("%s/%s", c.MetadataDir, id)
	oneWorkDir := fmt.Sprintf("%s/%s", c.WorkDir, id)
	isNeedWrite = true

	if err := fs.Mkdirp(oneMetadataDir); err != nil {
		return nil, fmt.Errorf("failed to create metadata dur: %s", err)
	}
	if err := fs.Mkdirp(oneWorkDir); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %s", err)
	}

	return &CommandConfig{
		WorkDir:   oneWorkDir,
		Script:    &WriterFile{Path: fmt.Sprintf("%s/script", oneMetadataDir), IsNeedWrite: isNeedWrite},
		Log:       &WriterFile{Path: fmt.Sprintf("%s/log", oneMetadataDir), IsNeedWrite: isNeedWrite},
		Env:       &WriterFile{Path: fmt.Sprintf("%s/env", oneMetadataDir), IsNeedWrite: isNeedWrite},
		StartAt:   &WriterFile{Path: fmt.Sprintf("%s/start_at", oneMetadataDir), IsNeedWrite: isNeedWrite},
		SucceedAt: &WriterFile{Path: fmt.Sprintf("%s/succeed_at", oneMetadataDir), IsNeedWrite: isNeedWrite},
		FailedAt:  &WriterFile{Path: fmt.Sprintf("%s/failed_at", oneMetadataDir), IsNeedWrite: isNeedWrite},
		Status:    &WriterFile{Path: fmt.Sprintf("%s/status", oneMetadataDir), IsNeedWrite: isNeedWrite},
		Error:     &WriterFile{Path: fmt.Sprintf("%s/error", oneMetadataDir), IsNeedWrite: isNeedWrite},
	}, nil
}

type WriterFile struct {
	Path        string
	IsNeedWrite bool
	//
	file *os.File
}

func (w *WriterFile) Write(p []byte) (n int, err error) {
	if !w.IsNeedWrite {
		return len(p), nil
	}

	if w.file == nil {
		if f, err := os.OpenFile(w.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err != nil {
			logger.Errorf("failed to open file: %s", err)
		} else {
			w.file = f
		}
	}

	if w.file != nil {
		return w.file.Write(p)
	}

	return len(p), nil
}

func (w *WriterFile) Close() error {
	if w.file != nil {
		return w.file.Close()
	}

	return nil
}

func (w *WriterFile) WriteString(content string) {
	if !w.IsNeedWrite {
		return
	}

	if err := fs.WriteFile(w.Path, []byte(content)); err != nil {
		logger.Errorf("failed to write file(%s): %s", w.Path, err)
	}
}

type server struct {
	cfg *Config
}

// New creates a new caas server
func New(cfg *Config) Server {
	if cfg.Shell == "" {
		cfg.Shell = DefaultShell
	}

	return &server{
		cfg: cfg,
	}
}

func (s *server) Run() error {
	app := defaults.Application()

	app.WebSocket("/", createWsService(s.cfg))

	return app.Run(fmt.Sprintf("0.0.0.0:%d", s.cfg.Port))
}
