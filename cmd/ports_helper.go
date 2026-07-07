package cmd

import (
	"devsandbox/core"
	"devsandbox/core/ports"
)

func loadSandboxPorts() ports.SandboxPorts {
	cwd := core.GetWorkspaceDir()
	p, err := ports.Load(cwd)
	if err != nil {
		return ports.Defaults()
	}
	return p
}
