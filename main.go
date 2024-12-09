package main

import (
	"fmt"
	"log"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
)

func main() {
	// config := &commandexecutor.SSHConfig{
	// 	Host:         "89.233.108.20",
	// 	Port:         22,
	// 	User:         "root",
	// 	Password:     "heS63uazKT",
	// 	SudoPassword: "heS63uazKT",
	// 	Timeout:      30 * time.Second,
	// }

	config := &commandexecutor.SSHConfig{
		Host:         "89.233.108.20",
		Port:         22,
		User:         "root",
		Password:     "heS63uazKt",
		SudoPassword: "heS63uazKt",
		Timeout:      30 * time.Second,
	}

	executor := commandexecutor.NewSSHExecutor(config)
	defer executor.Close()

	// Connect to remote server
	if err := executor.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	userService := NewUserService(executor)
	users, err := userService.List()
	if err != nil {
		log.Fatalf("Failed to list users: %v", err)
	}
	for _, user := range users {
		userInfo, err := userService.GetUserInfo(user)
		if err != nil {
			// log.Fatalf("Failed to get user info: %v", err)
			fmt.Println(user)
		}
		fmt.Println(userInfo)
	}

	// err = userService.Create(User{
	// 	Username: "test1",
	// 	Password: "HeS63uazKT",
	// 	Groups:   []string{"sudo"},
	// 	HomeDir:  "/home/test",
	// 	Shell:    "/bin/bash",
	// })
	// if err != nil {
	// 	log.Fatalf("Failed to create user: %v", err)
	// }

	serviceManager := NewServiceManager(executor, nil)
	services, err := serviceManager.ListServices()
	if err != nil {
		log.Fatalf("Failed to list services: %v", err)
	}
	fmt.Println(services)

	err = userService.setPassword("test1", "heS63uazKT")
	if err != nil {
		log.Fatalf("Failed to set password: %v", err)
	}

	// // Start a long-running service
	// serviceName := "ssh"
	// // if err := executor.ExecuteLongRunningService(serviceName, "start"); err != nil {
	// // 	log.Fatalf("Failed to start service: %v", err)
	// // }

	// // Monitor service status
	// statusChan, errChan := serviceManager.MonitorService(serviceName, 5*time.Second)

	// // Handle status updates and errors
	// go func() {
	// 	for {
	// 		select {
	// 		case status := <-statusChan:
	// 			fmt.Printf("Service status: %s\n", status)
	// 		case err := <-errChan:
	// 			fmt.Printf("Error monitoring service: %v\n", err)
	// 			return
	// 		}
	// 	}
	// }()

	// // // Wait for some time
	// time.Sleep(5 * time.Minute)

	// // Stop the service
	// if err := executor.ExecuteLongRunningService(serviceName, "stop"); err != nil {
	// 	log.Fatalf("Failed to stop service: %v", err)
	// }
}
