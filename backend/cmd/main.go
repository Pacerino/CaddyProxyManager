package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/Pacerino/CaddyProxyManager/internal/api"
	"github.com/Pacerino/CaddyProxyManager/internal/config"
	"github.com/Pacerino/CaddyProxyManager/internal/database"
	"github.com/Pacerino/CaddyProxyManager/internal/jobqueue"
	"github.com/Pacerino/CaddyProxyManager/internal/logger"
)

var (
	version = "3.0.0"
	commit  = "abcdefgh"
)

func main() {
	config.Init(&version, &commit)
	database.NewDB()
	jobqueue.Start()

	// HTTP Server
	api.StartServer()

	// Clean Quit
	irqchan := make(chan os.Signal, 1)
	signal.Notify(irqchan, syscall.SIGINT, syscall.SIGTERM)

	for irq := range irqchan {
		if irq == syscall.SIGINT || irq == syscall.SIGTERM {
			logger.Info("Got ", irq, " shutting server down ...")
			break
		}
	}
}
