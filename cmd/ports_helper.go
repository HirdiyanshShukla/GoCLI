package cmd

import (
	"os"

	"devsandbox/core/ports"
)

func loadSandboxPorts() ports.SandboxPorts {
	cwd, err := os.Getwd()
	if err != nil {
		return ports.Defaults()
	}
	p, err := ports.Load(cwd)
	if err != nil {
		return ports.Defaults()
	}
	return p
}
