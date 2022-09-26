package config

import (
	"fmt"
	golog "log"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"

	"github.com/vrischmann/envconfig"
)

// Version is the version set by ldflags
var Version string

// Commit is the git commit set by ldflags
var Commit string

// Init will parse environment variables into the Env struct
func Init(version, commit *string) {

	Version = *version
	Commit = *commit

	if err := envconfig.InitWithPrefix(&Configuration, "CPM"); err != nil {
		fmt.Printf("%+v\n", err)
	}
	initLogger()
	logger.Info("Build Version: %s (%s)", Version, Commit)
	createDataFolders()
}

// Init initialises the Log object and return it
func initLogger() {
	// this removes timestamp prefixes from logs
	golog.SetFlags(0)

	switch Configuration.Log.Level {
	case "debug":
		logLevel = logger.DebugLevel
	case "warn":
		logLevel = logger.WarnLevel
	case "error":
		logLevel = logger.ErrorLevel
	default:
		logLevel = logger.InfoLevel
	}

	err := logger.Configure(&logger.Config{
		LogThreshold: logLevel,
		Formatter:    Configuration.Log.Format,
	})

	if err != nil {
		logger.Error("LoggerConfigurationError", err)
	}
}

// GetLogLevel returns the logger const level
func GetLogLevel() logger.Level {
	return logLevel
}

/* func isError(errorClass string, err error) bool {
	if err != nil {
		logger.Error(errorClass, err)
		return true
	}
	return false
}
*/
