package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/simenandre/robin-user-api/internal/cli"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Stderr.WriteString("\ninterrupted\n")
		os.Exit(130)
	}()
	os.Exit(cli.Execute())
}
