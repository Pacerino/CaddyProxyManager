package caddy

import (
	"fmt"
	"os/exec"

	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

func ReloadCaddy() error {
	_, err := shExec([]string{"reload", "--config", config.Configuration.CaddyFile})
	return err
}

func getCaddyFilePath() (string, error) {
	path, err := exec.LookPath("caddy")
	if err != nil {
		return path, fmt.Errorf("cannot find caddy execuatable script in PATH")
	}
	return path, nil
}

// shExec executes caddy with arguments
func shExec(args []string) (string, error) {
	ng, err := getCaddyFilePath()
	if err != nil {
		logger.Error("CaddyError", err)
		return "", err
	}

	logger.Debug("CMD: %s %v", ng, args)
	// nolint: gosec
	c := exec.Command(ng, args...)

	b, e := c.Output()

	if e != nil {
		logger.Error("CaddyError", fmt.Errorf("command error: %s -- %v\n%+v", ng, args, e))
		logger.Warn(string(b))
	}

	return string(b), e
}
