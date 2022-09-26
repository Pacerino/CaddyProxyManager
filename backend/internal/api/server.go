package api

import (
	"fmt"
	"net/http"

	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

const httpPort = 3001

// StartServer creates a http server
func StartServer() {
	logger.Info("Server starting on port %v", httpPort)
	err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%v", httpPort), NewRouter())
	if err != nil {
		logger.Error("HttpListenError", err)
	}
}
