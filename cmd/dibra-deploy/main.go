package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	dibra_deploy_cli "github.com/gjergjiramku/dibra/internal/dibra-deploy-cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := dibra_deploy_cli.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
