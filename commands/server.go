package commands

import (
	"github.com/go-zoox/cli"
	"github.com/go-zoox/commands-as-a-service/server"
)

func RegistryServer(app *cli.MultipleProgram) {
	app.Register("server", &cli.Command{
		Name:  "server",
		Usage: "commands as a service server",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "port",
				Usage:   "server port",
				Aliases: []string{"p"},
				EnvVars: []string{"PORT"},
				Value:   8838,
			},
			&cli.StringFlag{
				Name:    "shell",
				Usage:   "specify command shell",
				Aliases: []string{"s"},
				EnvVars: []string{"CAAS_SHELL"},
				Value:   "sh",
			},
			&cli.StringFlag{
				Name:    "context",
				Usage:   "specify command context",
				Aliases: []string{"c"},
				EnvVars: []string{"CAAS_CONTEXT"},
			},
			&cli.StringFlag{
				Name:    "environment",
				Usage:   "specify command environment",
				Aliases: []string{"e"},
				EnvVars: []string{"CAAS_ENVIRONMENT"},
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
			&cli.Int64Flag{
				Name:    "timeout",
				Usage:   "specify command timeout, in seconds, default: 1800 (30 minutes)",
				Aliases: []string{"t"},
				EnvVars: []string{"CAAS_TIMEOUT"},
				Value:   1800,
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			return server.Serve(&server.Config{
				Port:     ctx.Int64("port"),
				Shell:    ctx.String("shell"),
				Context:  ctx.String("context"),
				Timeout:  ctx.Int64("timeout"),
				Username: ctx.String("username"),
				Password: ctx.String("password"),
			})
		},
	})
}
