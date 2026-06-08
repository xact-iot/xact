package main

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/xact-iot/xact/demo/iss_driver"
	"github.com/xact-iot/xact/demo/lta_driver"
	"github.com/xact-iot/xact/demo/lta_incidents_driver"
	"github.com/xact-iot/xact/demo/traffic_images_driver"
	"github.com/xact-iot/xact/demo/water_driver"
)

func main() {
	if err := loadDemoEnv(); err != nil {
		log.Printf("No .env file found, using environment variables")
	}

	go lta_driver.Start()
	go lta_incidents_driver.Start()
	go iss_driver.Start()
	go traffic_images_driver.Start()
	go water_driver.Start()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func loadDemoEnv() error {
	paths := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	var lastErr error
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if err := godotenv.Load(path); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New(".env not found")
	}
	return lastErr
}
