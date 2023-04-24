package client

import (
	"encoding/json"
	"fmt"
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
}

// Config is the configuration of caas client
type Config struct {
	Server       string
	ClientID     string
	ClientSecret string
}

type client struct {
	conn *websocket.Conn
	cfg  *Config
	//
	exitCode chan int
}

// New creates a new caas client
func New(cfg *Config) Client {
	return &client{
		cfg:      cfg,
		exitCode: make(chan int),
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
				logger.Errorf("failed to send ping: %s", err)
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
					conn.Close()
					return
				}

				logger.Errorf("failed to receive command response: %s", err)
				return
			}

			switch message[0] {
			case entities.MessageCommandStdout:
				os.Stdout.Write(message[1:])
			case entities.MessageCommandStderr:
				os.Stderr.Write(message[1:])
			case entities.MessageCommandExitCode:
				c.exitCode <- int(message[1])
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

	exitCode := <-c.exitCode

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("failed to close connection: %s", err)
	}

	os.Exit(exitCode)

	return nil
}
