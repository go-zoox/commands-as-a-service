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

type Client interface {
	Run(cfg *Config) error
}

type Config struct {
	Server      string
	Script      string
	Environment map[string]string
	Username    string
	Password    string
}

func Run(cfg *Config) (err error) {
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return fmt.Errorf("invalid caas server address: %s", err)
	}
	logger.Debugf("connecting to %s", u.String())

	client, response, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		body, errB := ioutil.ReadAll(response.Body)
		if errB != nil {
			return fmt.Errorf("failed to connect websocket at %s (status: %s, error: %s)", u.String(), response.Status, err)
		}

		return fmt.Errorf("failed to connect websocket at %s (status: %d, response: %s, error: %v)", u.String(), response.StatusCode, string(body), err)
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

	commandRequest := &entities.CommandRequest{
		Script:      cfg.Script,
		Environment: cfg.Environment,
	}
	message, err := json.Marshal(commandRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal command request: %s", err)
	}

	err = client.WriteMessage(websocket.TextMessage, append([]byte{entities.MessageCommand}, message...))
	if err != nil {
		return fmt.Errorf("failed to send command request: %s", err)
	}

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
		case entities.Stdout:
			os.Stdout.Write(message)
		case entities.Stderr:
			os.Stderr.Write(message)
		case entities.ExitCode:
			os.Exit(int(message[1]))
		}
	}
}
