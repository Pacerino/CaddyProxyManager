package api

import (
	"fmt"
	"net/http"

	"github.com/Pacerino/CaddyProxyManager/internal/api/handler"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

const httpPort = 3001

// StartServer serves the API backed by h.
func StartServer(h *handler.Handler) {
	logger.Info("Server starting on port %v", httpPort)
	err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%v", httpPort), NewRouter(h))
	if err != nil {
		logger.Error("HttpListenError", err)
	}
}
