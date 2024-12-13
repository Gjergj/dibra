package main

import (
	"fmt"
	"log"
	"time"

	"github.com/Gjergj/dibra/pkg/commandexecutor"
	"github.com/Gjergj/dibra/pkg/commandexecutor/cmdrunner"
)

func main() {

	config := &cmdrunner.SSHConfig{
		Host:       "localhost",
		Port:       32222,
		User:       "default",
		PrivateKey: "/Users/gjergjiramku/.orbstack/ssh/id_ed25519",
		// KeyPassphrase: "optional-passphrase", // Leave empty if key is not encrypted
		Timeout: 30 * time.Second,
		// KnownHosts:    "/Users/gjergjiramku/.orbstack/ssh/authorized_keys",
		AllowInsecure: true,
	}

	sshConnection := cmdrunner.NewSSHConnection(config)
	defer sshConnection.Close()

	// Connect to remote server
	if err := sshConnection.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	commandExecutor := commandexecutor.NewCommandRunner(sshConnection)
	// userService := NewUserService(commandExecutor, nil)

	// users, err := userService.List()
	// if err != nil {
	// 	log.Fatalf("Failed to list users: %v", err)
	// }
	// for _, user := range users {
	// 	userInfo, err := userService.GetUserInfo(user)
	// 	if err != nil {
	// 		// log.Fatalf("Failed to get user info: %v", err)
	// 		fmt.Println(user)
	// 	}
	// 	fmt.Println(userInfo)
	// }

	// err = userService.Create(User{
	// 	Username: "test2",
	// 	Password: "heS63uazKz",
	// 	Groups:   []string{"sudo"},
	// 	HomeDir:  "/home/tes2t",
	// 	Shell:    "/bin/bash",
	// })
	// if err != nil {
	// 	log.Fatalf("Failed to create user: %v", err)
	// }

	serviceManager := NewServiceManager(commandExecutor, &commandexecutor.SudoInfo{Password: "heS63uazKT"})
	services, err := serviceManager.ListServices()
	if err != nil {
		log.Fatalf("Failed to list services: %v", err)
	}
	fmt.Println(services)

	// Create a new service unit
	unit := ServiceUnit{
		Name:        "myapp",
		Description: "My Custom Application Service",
		ExecStart:   "/usr/local/bin/myapp",
		WorkingDir:  "/opt/myapp",
		User:        "myappuser",
		Environment: []string{"PORT=8080", "ENV=production"},
		Restart:     "always",
		WantedBy:    "multi-user.target",
	}

	// Create and install the service
	if err := serviceManager.CreateServiceUnit(unit); err != nil {
		log.Fatalf("Failed to create service unit: %v", err)
	}

	if err := serviceManager.InstallService("myapp"); err != nil {
		log.Fatalf("Failed to install service: %v", err)
	}

	// Start the service
	if err := serviceManager.StartService("myapp"); err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}

	// ... later, if you need to stop the service ...
	if err := serviceManager.StopService("myapp"); err != nil {
		log.Fatalf("Failed to stop service: %v", err)
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
