package client

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"net/url"

	"github.com/go-zoox/commands-as-a-service/entities"
	"github.com/go-zoox/logger"
	"github.com/gorilla/websocket"
)

// Client is the interface of caas client
type Client interface {
	Connect() error
	Exec(command *entities.Command) error
	Close() error
}

// Config is the configuration of caas client
type Config struct {
	Server       string `config:"server"`
	ClientID     string `config:"client_id"`
	ClientSecret string `config:"client_secret"`
	AutoExit     bool   `config:"auto_exit"`
	//
	Stdout io.Writer
	Stderr io.Writer
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

	return &client{
		cfg:      cfg,
		exitCode: make(chan int),
		stdout:   stdout,
		stderr:   stderr,
	}
}

func (c *client) Connect() (err error) {
	ready := make(chan struct{})

	u, err := url.Parse(c.cfg.Server)
	if err != nil {
		return fmt.Errorf("invalid caas server address: %s", err)
	}
	logger.Debugf("connecting to %s", u.String())

	conn, response, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		if response == nil || response.Body == nil {
			return fmt.Errorf("failed to connect at %s (error: %s)", u.String(), err)
		}

		body, errB := ioutil.ReadAll(response.Body)
		if errB != nil {
			return fmt.Errorf("failed to connect at %s (status: %s, error: %s)", u.String(), response.Status, err)
		}

		return fmt.Errorf("failed to connect at %s (status: %d, response: %s, error: %v)", u.String(), response.StatusCode, string(body), err)
	}
	c.conn = conn

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
				c.exitCode <- 1
			case entities.MessageAuthResponseSuccess:
				ready <- struct{}{}
			}
		}
	}()

	<-ready

	return nil
}

func (c *client) Exec(command *entities.Command) error {
	// command request
	commandRequest := &entities.Command{
		Script:      command.Script,
		Environment: command.Environment,
	}
	message, err := json.Marshal(commandRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal command request: %s", err)
	}
	err = c.conn.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageCommand}, message...))
	if err != nil {
		return fmt.Errorf("failed to send command request: %s", err)
	}

	// <-c.exitCode
	exitCode := <-c.exitCode

	if err := c.conn.Close(); err != nil {
		// return fmt.Errorf("failed to close connection: %s", err)
		logger.Debugf("failed to close connection: %s", err)
	}

	if c.cfg.AutoExit {
		os.Exit(exitCode)
	}

	return nil
}

func (c *client) Close() error {
	return c.conn.Close()
}
