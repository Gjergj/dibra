package main

import (
	dibra_deploy_cli "github.com/gjergjiramku/dibra/internal/dibra-deploy-cli"
)

func main() {
	if err := dibra_deploy_cli.Execute(); err != nil{
		panic(err)
	}
}
