package config

import (
	"fmt"
	"os"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

func createDataFolders() {
	folders := []string{
		"hosts",
	}

	for _, folder := range folders {
		path := folder
		if path[0:1] != "/" {
			path = fmt.Sprintf("%s/%s", Configuration.DataFolder, folder)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// folder does not exist
			logger.Debug("Creating folder: %s", path)
			if err := os.MkdirAll(path, os.ModePerm); err != nil {
				logger.Error("CreateDataFolderError", err)
			}
		}
	}
}
