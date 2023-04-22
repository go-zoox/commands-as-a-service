package commands

import (
	"fmt"

	"github.com/go-zoox/cli"
	"github.com/go-zoox/commands-as-a-service/client"
	"github.com/go-zoox/fs"
)

func RegistryClient(app *cli.MultipleProgram) {
	app.Register("client", &cli.Command{
		Name:  "client",
		Usage: "commands as a service client",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "server",
				Usage:    "server url",
				Aliases:  []string{"s"},
				EnvVars:  []string{"CAAS_SERVER"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "script",
				Usage:   "specify command script",
				EnvVars: []string{"CAAS_SCRIPT"},
				// Required: true,
			},
			&cli.StringFlag{
				Name:    "username",
				Usage:   "Username for Basic Auth",
				EnvVars: []string{"CAAS_USERNAME"},
			},
			&cli.StringFlag{
				Name:    "password",
				Usage:   "Password for Basic Auth",
				EnvVars: []string{"CAAS_PASSWORD"},
			},
			//
			&cli.StringFlag{
				Name:    "script-path",
				Usage:   "specify command script path",
				EnvVars: []string{"CAAS_SCRIPT_PATH"},
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			script := ctx.String("script")
			if scriptPath := ctx.String("script-path"); scriptPath != "" {
				if ok := fs.IsExist(scriptPath); !ok {
					return fmt.Errorf("script path not found: %s", scriptPath)
				}

				if scriptText, err := fs.ReadFileAsString(scriptPath); err != nil {
					return fmt.Errorf("failed to read script file: %s", err)
				} else {
					script = scriptText
				}
			}

			if script == "" {
				return fmt.Errorf("script is required")
			}

			return client.Run(&client.Config{
				Server:   ctx.String("server"),
				Script:   script,
				Username: ctx.String("username"),
				Password: ctx.String("password"),
			})
		},
	})
}
