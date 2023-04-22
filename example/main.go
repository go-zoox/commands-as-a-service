package main

import (
	"github.com/go-zoox/cli"
)

func main() {
	app := cli.NewMultipleProgram(&cli.MultipleProgramConfig{
		Name:  "command-as-a-service",
		Usage: "command-as-a-service is a portable command as a service",
	})

	// server
	registryServer(app)
	// client
	registryClient(app)

	app.Run()
}
