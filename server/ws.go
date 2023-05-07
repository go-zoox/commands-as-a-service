package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/go-zoox/commands-as-a-service/entities"
	"github.com/go-zoox/datetime"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/zoox"
	"github.com/go-zoox/zoox/components/context/websocket"
)

// WSClientWriter is the writer for websocket client
type WSClientWriter struct {
	io.Writer
	Client *websocket.WebSocketClient
	Flag   byte
}

func (w WSClientWriter) Write(p []byte) (n int, err error) {
	w.Client.WriteText(append([]byte{w.Flag}, p...))
	return len(p), nil
}

func createWsService(cfg *Config) func(ctx *zoox.Context, client *websocket.WebSocketClient) {
	heartbeatTimeout := 30 * time.Second
	authenticator := createAuthenticator(cfg)

	return func(ctx *zoox.Context, client *websocket.WebSocketClient) {
		var cmd *exec.Cmd
		var authClient *entities.AuthRequest
		var command *entities.Command

		isAuthenticated := false
		stopped := false
		isKilledByDisconnect := false
		if cfg.ClientID == "" && cfg.ClientSecret == "" && cfg.AuthService == "" {
			isAuthenticated = true
		}

		authenticationTimeoutTimer := time.AfterFunc(30*time.Second, func() {
			if !isAuthenticated {
				logger.Debugf("[ws][id: %s] authentication timeout", client.ID)

				client.Disconnect()
			}
		})
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
				return
			case entities.MessageAuthRequest:
				logger.Infof("[ws][id: %s] auth request", client.ID)
				authClient = &entities.AuthRequest{}
				if err := json.Unmarshal(msg[1:], authClient); err != nil {
					logger.Errorf("[ws][id: %s] failed to unmarshal auth request: %s", client.ID, err)
					return
				}
				authenticationTimeoutTimer.Stop()
				if err := authenticator(authClient.ClientID, authClient.ClientSecret); err != nil {
					logger.Errorf("[ws][id: %s] failed to authenticate => %v", client.ID, err)

					client.WriteText(append([]byte{entities.MessageAuthResponseFailure}, []byte(fmt.Sprintf("failed to authenticate: %s\n", err))...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					client.Disconnect()
					return
				}

				isAuthenticated = true
				logger.Infof("[ws][id: %s] authenticated", client.ID)
				client.WriteText([]byte{entities.MessageAuthResponseSuccess})
			case entities.MessageCommand:
				if !isAuthenticated {
					logger.Errorf("[ws][id: %s] not authenticated", client.ID)
					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte("not authenticated\n")...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					client.Disconnect()
					return
				}

				command = &entities.Command{}
				if err := json.Unmarshal(msg[1:], command); err != nil {
					logger.Errorf("failed to unmarshal command request: %s", err)

					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte("invalid command request\n")...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					return
				}

				cmdCfg, err := cfg.GetCommandConfig(client.ID)
				if err != nil {
					logger.Errorf("failed to get command config: %s", err)
					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte("internal server error\n")...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					return
				}

				cmd = exec.Command(cfg.Shell, "-c", command.Script)
				cmd.Dir = cmdCfg.WorkDir
				// cmd.Env = []string{}
				environment := map[string]string{}
				if command.Environment != nil {
					for k, v := range command.Environment {
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

				cmd.Stdout = io.MultiWriter(cmdCfg.Log, &WSClientWriter{Client: client, Flag: entities.MessageCommandStdout})
				cmd.Stderr = io.MultiWriter(cmdCfg.Log, &WSClientWriter{Client: client, Flag: entities.MessageCommandStderr})

				logger.Infof("[command] start to run: %s", command.Script)
				cmdCfg.Script.WriteString(command.Script)
				cmdCfg.Env.WriteString(strings.Join(cmd.Env, "\n"))
				cmdCfg.StartAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
				err = cmd.Run()
				if err != nil {
					if isKilledByDisconnect {
						logger.Infof("[command] killed by disconnect: %s", command.Script)
						return
					}

					cmdCfg.FailedAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
					cmdCfg.Error.WriteString(err.Error())
					cmdCfg.Status.WriteString("failure")

					logger.Errorf("[command] failed to run: %s (err: %v, exit code: %d)", command.Script, err, cmd.ProcessState.ExitCode())
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(cmd.ProcessState.ExitCode())})
					return
				}
				client.WriteText([]byte{entities.MessageCommandExitCode, byte(0)})

				cmdCfg.SucceedAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
				cmdCfg.Status.WriteString("success")
				logger.Infof("[command] succeed to run: %s", command.Script)

				commandTimeoutTimer.Stop()
				heartbeatTimeoutTimer.Stop()

				stopped = true
			default:
				logger.Errorf("unknown message type: %d", msg[0])
			}
		}
	}
}
