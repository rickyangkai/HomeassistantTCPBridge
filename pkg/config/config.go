package config

import (
	"encoding/json"
	"log"
	"os"
	"strings"
)

type Options struct {
	ClientIPWhitelist        string `json:"client_ip_whitelist"`
	EnableGenericCallService bool   `json:"enable_generic_call_service"`
	UseTLS                   bool   `json:"use_tls"`
}

type Config struct {
	SupervisorToken string
	HAWebSocketURL  string
	Options         Options
	Whitelist       []string
}

func Load() *Config {
	// 1. Load Environment Variables
	token := os.Getenv("SUPERVISOR_TOKEN")
	if token == "" {
		log.Println("Warning: SUPERVISOR_TOKEN not found in environment")
	}

	// 2. Load Options from JSON
	optionsFile := "/data/options.json"
	// Fallback for local development if file doesn't exist
	if _, err := os.Stat(optionsFile); os.IsNotExist(err) {
		optionsFile = "options.json" // Try local dir
	}

	var opts Options
	if _, err := os.Stat(optionsFile); err == nil {
		content, err := os.ReadFile(optionsFile)
		if err != nil {
			log.Printf("Error reading options file: %v", err)
		} else {
			if err := json.Unmarshal(content, &opts); err != nil {
				log.Printf("Error parsing options file: %v", err)
			} else {
				log.Printf("Loaded configuration from %s", optionsFile)
			}
		}
	} else {
		log.Println("No options.json found, using defaults")
	}

	// 3. Parse Whitelist
	var whitelist []string
	if opts.ClientIPWhitelist != "" {
		parts := strings.Split(opts.ClientIPWhitelist, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				whitelist = append(whitelist, trimmed)
			}
		}
	}

	return &Config{
		SupervisorToken: token,
		HAWebSocketURL:  "ws://supervisor/core/api/websocket", // Default for HAOS
		Options:         opts,
		Whitelist:       whitelist,
	}
}
