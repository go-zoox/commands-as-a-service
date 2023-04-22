package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/go-zoox/commands-as-a-service/entities"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/zoox"
	"github.com/go-zoox/zoox/components/context/websocket"
	gw "github.com/gorilla/websocket"
)

type WSClientWriter struct {
	io.Writer
	Client *websocket.WebSocketClient
	Flag   byte
}

func (w WSClientWriter) Write(p []byte) (n int, err error) {
	w.Client.WriteBinary(append([]byte{w.Flag}, p...))
	return len(p), nil
}

func createWsService(cfg *Config) func(ctx *zoox.Context, client *websocket.WebSocketClient) {
	heartbeatTimeout := 30 * time.Second
	return func(ctx *zoox.Context, client *websocket.WebSocketClient) {
		var cmd *exec.Cmd
		stopped := false
		isKilledByDisconnect := false

		heartbeatTimeoutTimer := time.AfterFunc(heartbeatTimeout, func() {
			logger.Debugf("[ws][id: %s] heart beat timeout", client.ID)

			client.Disconnect()
		})

		client.OnConnect = func() {
			logger.Debugf("[ws][id: %s] connect", client.ID)
		}

		client.OnDisconnect = func() {
			logger.Debugf("[ws][id: %s] disconnect", client.ID)

			if cmd != nil && !stopped {
				isKilledByDisconnect = true

				cmd.Process.Kill()
			}
		}

		client.OnTextMessage = func(msg []byte) {
			switch msg[0] {
			case entities.MessagePing:
				logger.Debugf("[ws][id: %s] receive ping", client.ID)
				heartbeatTimeoutTimer.Reset(heartbeatTimeout)
			case entities.MessageCommand:
				data := &entities.CommandRequest{}
				if err := json.Unmarshal(msg[1:], data); err != nil {
					logger.Errorf("failed to unmarshal command request: %s", err)

					client.WriteBinary(append([]byte{entities.Stderr}, []byte("invalid command request")...))
					client.WriteBinary([]byte{entities.ExitCode, byte(1)})
					client.Disconnect()
					return
				}

				cmd = exec.Command(cfg.Shell, "-c", data.Script)
				cmd.Dir = cfg.Context
				// cmd.Env = []string{}
				environment := map[string]string{}
				if data.Environment != nil {
					for k, v := range data.Environment {
						environment[k] = v
					}
				}
				if cfg.Environment != nil {
					for k, v := range cfg.Environment {
						environment[k] = v
					}
				}
				for k, v := range environment {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
				}

				// timeout
				commandTimeoutTimer := time.AfterFunc(time.Duration(cfg.Timeout)*time.Second, func() {
					cmd.Process.Kill()
				})

				cmd.Stdout = &WSClientWriter{Client: client, Flag: entities.Stdout}
				cmd.Stderr = &WSClientWriter{Client: client, Flag: entities.Stderr}

				logger.Infof("[command] start to run: %s", data.Script)
				err := cmd.Run()
				if err != nil {
					if isKilledByDisconnect {
						logger.Infof("[command] killed by disconnect: %s", data.Script)
						return
					}

					logger.Errorf("[command] failed to run: %s (err: %v, exit code: %d)", data.Script, err, cmd.ProcessState.ExitCode())
					client.WriteBinary([]byte{entities.ExitCode, byte(cmd.ProcessState.ExitCode())})
				}
				logger.Infof("[command] succeed to run: %s", data.Script)

				commandTimeoutTimer.Stop()
				heartbeatTimeoutTimer.Stop()

				client.WriteMessage(gw.CloseMessage, gw.FormatCloseMessage(1000, "woops"))
				client.Disconnect()
				stopped = true
			default:
				logger.Errorf("unknown message type: %d", msg[0])
				client.Disconnect()
			}
		}
	}
}
