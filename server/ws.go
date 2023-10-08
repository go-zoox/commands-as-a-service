package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	// "os/exec"
	"strings"
	"time"

	"github.com/go-zoox/command"
	"github.com/go-zoox/command/errors"
	"github.com/go-zoox/commands-as-a-service/entities"
	"github.com/go-zoox/datetime"
	"github.com/go-zoox/fs"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/zoox"
	"github.com/go-zoox/zoox/components/application/websocket"
)

// WSClientWriter is the writer for websocket client
type WSClientWriter struct {
	io.Writer
	Client *websocket.Client
	Flag   byte
}

func (w WSClientWriter) Write(p []byte) (n int, err error) {
	w.Client.WriteText(append([]byte{w.Flag}, p...))
	return len(p), nil
}

func createWsService(cfg *Config) func(ctx *zoox.Context, client *websocket.Client) {
	heartbeatTimeout := 30 * time.Second
	authenticator := createAuthenticator(cfg)

	return func(ctx *zoox.Context, client *websocket.Client) {
		var cmd command.Command
		var authClient *entities.AuthRequest
		var commandN *entities.Command

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

				if cmd != nil {
					cmd.Cancel()
				}
			}
		}

		client.OnTextMessage = func(msg []byte) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[ws][id: %s] receive text message panic => %v", client.ID, r)
					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte(fmt.Sprintf("internal server error: %v\n", r))...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					return
				}
			}()

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

				commandN = &entities.Command{}
				tmpScriptFilepath := ""
				if err := json.Unmarshal(msg[1:], commandN); err != nil {
					logger.Errorf("failed to unmarshal command request: %s", err)

					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte("invalid command request\n")...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					return
				}

				id := client.ID
				if commandN.ID != "" {
					id = commandN.ID
				}
				cmdCfg, err := cfg.GetCommandConfig(id, commandN)
				if err != nil {
					logger.Errorf("failed to get command config: %s", err)
					client.WriteText(append([]byte{entities.MessageCommandStderr}, []byte("internal server error\n")...))
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(1)})
					return
				}
				defer func() {
					// @TODO clean workdir
					if cfg.IsAutoCleanWorkDir {
						if fs.IsExist(cmdCfg.WorkDir) {
							logger.Infof("[command] clean work dir: %s", cmdCfg.WorkDir)
							if err := fs.Remove(cmdCfg.WorkDir); err != nil {
								panic(fmt.Errorf("failed to clean workdir(%s): %s", cmdCfg.WorkDir, err))
							}
						}
					}
				}()

				env := []string{}
				environment := map[string]string{
					"HOME":    os.Getenv("HOME"),
					"USER":    os.Getenv("USER"),
					"LOGNAME": os.Getenv("LOGNAME"),
					"SHELL":   cfg.Shell,
					"TERM":    os.Getenv("TERM"),
					"PATH":    os.Getenv("PATH"),
				}
				if commandN.Environment != nil {
					for k, v := range commandN.Environment {
						environment[k] = v
					}
				}
				if cfg.Environment != nil {
					for k, v := range cfg.Environment {
						environment[k] = v
					}
				}
				for k, v := range environment {
					env = append(env, fmt.Sprintf("%s=%s", k, v))
				}

				if cfg.Shell == DefaultShell {
					// cmd = exec.Command(cfg.Shell, "-c", command.Script)
					cmd, err = command.New(&command.Config{
						Command:     commandN.Script,
						Shell:       cfg.Shell,
						WorkDir:     cmdCfg.WorkDir,
						Environment: environment,
						User:        commandN.User,
					})
					if err != nil {
						panic(fmt.Errorf("failed to create command (1): %s", err))
					}
				} else {
					// file mode
					tmpScriptFilepath = fs.TmpFilePath()
					// logger.Infof("[script_mode: %s] tmp script filepath: %s", cfg.ScriptMode, tmpScriptFilepath)
					if err := fs.WriteFile(tmpScriptFilepath, []byte(commandN.Script)); err != nil {
						panic(fmt.Errorf("failed to write script file: %s", err))
					}

					// cmd = exec.Command(cfg.Shell, tmpScriptFilepath)
					cmd, err = command.New(&command.Config{
						Command: fmt.Sprintf("%s %s", cfg.Shell, commandN.Script),
						// Shell:   cfg.Shell,
						WorkDir:     cmdCfg.WorkDir,
						Environment: environment,
						User:        commandN.User,
					})
					if err != nil {
						panic(fmt.Errorf("failed to create command (2): %s", err))
					}
				}

				// timeout
				var commandTimeoutTimer *time.Timer
				if cfg.Timeout != 0 {
					commandTimeoutTimer = time.AfterFunc(time.Duration(cfg.Timeout)*time.Second, func() {
						if cmd != nil {
							cmd.Cancel()
						}
					})
				}

				cmd.SetStdout(io.MultiWriter(cmdCfg.Log, &WSClientWriter{Client: client, Flag: entities.MessageCommandStdout}))
				cmd.SetStderr(io.MultiWriter(cmdCfg.Log, &WSClientWriter{Client: client, Flag: entities.MessageCommandStderr}))

				logger.Infof("[command] start to run: %s", commandN.Script)
				cmdCfg.Script.WriteString(commandN.Script)
				cmdCfg.Env.WriteString(strings.Join(env, "\n"))
				cmdCfg.StartAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
				err = cmd.Run()
				if err != nil {
					if isKilledByDisconnect {
						logger.Infof("[command] killed by disconnect: %s", commandN.Script)
						return
					}

					cmdCfg.FailedAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
					cmdCfg.Error.WriteString(err.Error())
					cmdCfg.Status.WriteString("failure")

					exitCode := -1
					if errx, ok := err.(*errors.ExitError); ok {
						exitCode = errx.ExitCode()
					}

					logger.Errorf("[command] failed to run: %s (err: %v, exit code: %d)", commandN.Script, err, exitCode)
					client.WriteText([]byte{entities.MessageCommandExitCode, byte(exitCode)})
					return
				}

				client.WriteText([]byte{entities.MessageCommandExitCode, byte(0)})

				cmdCfg.SucceedAt.WriteString(datetime.Now().Format("YYYY-MM-DD HH:mm:ss"))
				cmdCfg.Status.WriteString("success")
				logger.Infof("[command] succeed to run: %s", commandN.Script)

				if tmpScriptFilepath != "" && fs.IsExist(tmpScriptFilepath) {
					if err := fs.Remove(tmpScriptFilepath); err != nil {
						panic(fmt.Errorf("failed to remove tmp script file: %s", err))
					}
				}

				if commandTimeoutTimer != nil {
					commandTimeoutTimer.Stop()
				}

				if heartbeatTimeoutTimer != nil {
					heartbeatTimeoutTimer.Stop()
				}

				stopped = true
			default:
				logger.Errorf("unknown message type: %d", msg[0])
			}
		}
	}
}
