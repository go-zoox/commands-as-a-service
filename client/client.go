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
	Run() error
}

// Config is the configuration of caas client
type Config struct {
	Server       string
	Script       string
	Environment  map[string]string
	ClientID     string
	ClientSecret string
}

type client struct {
	cfg *Config
}

// New creates a new caas client
func New(cfg *Config) Client {
	return &client{
		cfg: cfg,
	}
}

func (c *client) Run() (err error) {
	u, err := url.Parse(c.cfg.Server)
	if err != nil {
		return fmt.Errorf("invalid caas server address: %s", err)
	}
	logger.Debugf("connecting to %s", u.String())

	client, response, err := websocket.DefaultDialer.Dial(u.String(), nil)
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
	defer client.Close()

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
	}(client)

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
		err = client.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageAuthRequest}, message...))
		if err != nil {
			logger.Errorf("failed to send auth request: %s", err)
		}
	}()

	for {
		_, message, err := client.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				client.Close()
				return nil
			}

			return fmt.Errorf("failed to receive command response: %s", err)
		}

		switch message[0] {
		case entities.MessageCommandStdout:
			os.Stdout.Write(message[1:])
		case entities.MessageCommandStderr:
			os.Stderr.Write(message[1:])
		case entities.MessageCommandExitCode:
			os.Exit(int(message[1]))
		case entities.MessageAuthResponseSuccess:
			// command request
			commandRequest := &entities.CommandRequest{
				Script:      c.cfg.Script,
				Environment: c.cfg.Environment,
			}
			message, err = json.Marshal(commandRequest)
			if err != nil {
				return fmt.Errorf("failed to marshal command request: %s", err)
			}
			err = client.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageCommand}, message...))
			if err != nil {
				return fmt.Errorf("failed to send command request: %s", err)
			}
		}
	}
}
