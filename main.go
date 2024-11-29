package main

import (
	"fmt"
	"log"
	"time"
)

func main() {
	config := &SSHConfig{
		Host:         "89.233.108.20",
		Port:         22,
		User:         "root",
		Password:     "heS63uazKt",
		SudoPassword: "heS63uazKt",
		Timeout:      30 * time.Second,
	}

	executor := NewSSHExecutor(config)
	defer executor.Close()

	// Connect to remote server
	if err := executor.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	services, err := executor.ListServices()
	if err != nil {
		log.Fatalf("Failed to list services: %v", err)
	}
	fmt.Println(services)

	// Start a long-running service
	serviceName := "ssh"
	// if err := executor.ExecuteLongRunningService(serviceName, "start"); err != nil {
	// 	log.Fatalf("Failed to start service: %v", err)
	// }

	// Monitor service status
	statusChan, errChan := executor.MonitorService(serviceName, 5*time.Second)

	// Handle status updates and errors
	go func() {
		for {
			select {
			case status := <-statusChan:
				fmt.Printf("Service status: %s\n", status)
			case err := <-errChan:
				fmt.Printf("Error monitoring service: %v\n", err)
				return
			}
		}
	}()

	// // Wait for some time
	time.Sleep(5 * time.Minute)

	// // Stop the service
	// if err := executor.ExecuteLongRunningService(serviceName, "stop"); err != nil {
	// 	log.Fatalf("Failed to stop service: %v", err)
	// }
}
