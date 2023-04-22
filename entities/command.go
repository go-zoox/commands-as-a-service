package entities

type CommandRequest struct {
	Script      string            `json:"script"`
	Environment map[string]string `json:"environment"`
	Context     string            `json:"context"`
}

type CommandResponse struct {
	// api
	Code    int    `json:"code"`
	Message string `json:"message"`
	// command
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}
