package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"net/url"

	"github.com/go-zoox/commands-as-a-service/entities"
	"github.com/go-zoox/core-utils/strings"
	"github.com/go-zoox/logger"
	"github.com/gorilla/websocket"
)

// Client is the interface of caas client
type Client interface {
	Connect() error
	Exec(command *entities.Command) error
	Close() error
	//
	Output(command *entities.Command) (response string, err error)
	//
	TerminalURL(path ...string) string
}

type ExitError struct {
	ExitCode int
	Message  string
}

func (e ExitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("exit code: %d, message: %s", e.ExitCode, e.Message)
	}

	return fmt.Sprintf("exit code: %d", e.ExitCode)
}

// Config is the configuration of caas client
type Config struct {
	Server       string `config:"server"`
	ClientID     string `config:"client_id"`
	ClientSecret string `config:"client_secret"`
	//
	Stdout io.Writer
	Stderr io.Writer
	//
	ExecTimeout time.Duration `config:"exec_timeout"`
}

type client struct {
	conn *websocket.Conn
	cfg  *Config
	//
	exitCode chan int
	//
	stdout io.Writer
	stderr io.Writer
}

// New creates a new caas client
func New(cfg *Config) Client {
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if cfg.ExecTimeout == 0 {
		cfg.ExecTimeout = 7 * 24 * time.Hour
	}

	return &client{
		cfg:      cfg,
		exitCode: make(chan int),
		stdout:   stdout,
		stderr:   stderr,
	}
}

func (c *client) Connect() (err error) {
	errCh := make(chan error)

	u, err := url.Parse(c.cfg.Server)
	if err != nil {
		return fmt.Errorf("invalid caas server address: %s", err)
	}
	logger.Debugf("connecting to %s", u.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		if response == nil || response.Body == nil {
			cancel()
			return fmt.Errorf("failed to connect at %s (error: %s)", u.String(), err)
		}

		body, errB := ioutil.ReadAll(response.Body)
		if errB != nil {
			cancel()
			return fmt.Errorf("failed to connect at %s (status: %s, error: %s)", u.String(), response.Status, err)
		}

		cancel()
		return fmt.Errorf("failed to connect at %s (status: %d, response: %s, error: %v)", u.String(), response.StatusCode, string(body), err)
	}
	c.conn = conn
	cancel()

	// heart beat
	go func(c *websocket.Conn) {
		for {
			time.Sleep(3 * time.Second)

			logger.Debugf("ping")
			if err := c.WriteMessage(websocket.TextMessage, []byte{entities.MessagePing}); err != nil {
				logger.Debugf("failed to send ping: %s", err)
				return
			}
		}
	}(conn)

	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return
				}

				if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
					return
				}

				if websocket.IsCloseError(err, websocket.CloseGoingAway) {
					return
				}

				logger.Debugf("failed to receive command response: %s", err)
				// os.Exit(1)
				return
			}

			switch message[0] {
			case entities.MessageCommandStdout:
				c.stdout.Write(message[1:])
			case entities.MessageCommandStderr:
				c.stderr.Write(message[1:])
			case entities.MessageCommandExitCode:
				c.exitCode <- int(message[1])
			case entities.MessageAuthResponseFailure:
				c.stderr.Write(message[1:])
				// c.exitCode <- 1
				errCh <- &ExitError{
					ExitCode: 1,
					Message:  string(message[1:]),
				}
			case entities.MessageAuthResponseSuccess:
				errCh <- nil
			}
		}
	}()

	// auth request
	go func() {
		time.Sleep(10 * time.Millisecond)
		authRequest := &entities.AuthRequest{
			ClientID:     c.cfg.ClientID,
			ClientSecret: c.cfg.ClientSecret,
		}
		message, err := json.Marshal(authRequest)
		if err != nil {
			logger.Errorf("failed to marshal auth request: %s", err)
		}
		err = conn.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageAuthRequest}, message...))
		if err != nil {
			logger.Errorf("failed to send auth request: %s", err)
		}
	}()

	return <-errCh
}

func (c *client) Exec(command *entities.Command) error {
	go func() {
		time.AfterFunc(c.cfg.ExecTimeout, func() {
			c.stderr.Write([]byte("command exec timeout\n"))
			c.exitCode <- 1
		})
	}()

	message, err := json.Marshal(command)
	if err != nil {
		return &ExitError{
			ExitCode: 1,
			Message:  fmt.Sprintf("failed to marshal command request: %s", err),
		}
	}
	err = c.conn.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageCommand}, message...))
	if err != nil {
		return &ExitError{
			ExitCode: 1,
			Message:  fmt.Sprintf("failed to send command request: %s", err),
		}
	}

	exitCode := <-c.exitCode

	if exitCode == 0 {
		return nil
	}

	return &ExitError{
		ExitCode: exitCode,
	}
}

func (c *client) Output(command *entities.Command) (response string, err error) {
	responseBuf := NewBufWriter()

	c.stdout = responseBuf
	c.stderr = responseBuf
	if err = c.Exec(command); err != nil {
		return strings.TrimSpace(responseBuf.String()), nil
	}

	if err = c.Close(); err != nil {
		return
	}

	return strings.TrimSpace(responseBuf.String()), nil
}

func (c *client) Close() error {
	return c.conn.Close()
}

func (c *client) TerminalURL(path ...string) string {
	terminalPath := "/terminal"
	if len(path) > 0 && path[0] != "" {
		terminalPath = path[0]
	}

	u, err := url.Parse(c.cfg.Server)
	if err != nil {
		return ""
	}

	u.Path = terminalPath
	return u.String()
}

func NewBufWriter() *BufWriter {
	return &BufWriter{
		buf: &bytes.Buffer{},
	}
}

type BufWriter struct {
	io.Writer
	buf *bytes.Buffer
}

func (w *BufWriter) Write(p []byte) (n int, err error) {
	return w.buf.Write(p)
}

func (w *BufWriter) String() string {
	return w.buf.String()
}
