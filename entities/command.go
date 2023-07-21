package entities

// Command is the request for command
type Command struct {
	Script      string            `json:"script"`
	Environment map[string]string `json:"environment"`
	WorkDirBase string            `json:"workdirbase"`
}
