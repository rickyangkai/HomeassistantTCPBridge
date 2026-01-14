package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rick/homeassistant-tcp-bridge/pkg/config"
	"github.com/rick/homeassistant-tcp-bridge/pkg/ha"
	"github.com/rick/homeassistant-tcp-bridge/pkg/savant"
)

func main() {
	log.Println("Starting Home Assistant <-> Savant Bridge (Go Version)...")

	// 1. Load Config
	cfg := config.Load()

	// 2. Initialize Components
	// We need a circular dependency resolution: Savant Server needs HA Client to send commands,
	// HA Client needs a callback to send updates to Savant Server.
	
	// Create channels or use a forward declaration approach.
	// In Go, we can pass a function closure.
	
	var savantServer *savant.Server

	onHAMessage := func(msg string) {
		if savantServer != nil {
			savantServer.Broadcast(msg)
		}
	}

	haClient := ha.NewClient(cfg.HAWebSocketURL, cfg.SupervisorToken, onHAMessage)
	savantServer = savant.NewServer(8080, cfg, haClient)

	// 3. Start Services
	haClient.Start()
	go savantServer.Start()

	// 4. Wait for Signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
}
