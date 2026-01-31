package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/davidseybold/rpz-loader/internal/app"
)

func main() {
	configPath := flag.String("config", "/etc/rpz-loader/config.yaml", "path to config file")
	flag.Parse()

	if err := app.Run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
