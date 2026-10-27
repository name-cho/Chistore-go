package main

import (
	"context"
	_ "embed"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/name-cho/chistore-go/internal/app"
)

//go:embed VERSION
var versionFile string

var version = ""

func init() {
	if version == "" {
		version = strings.TrimSpace(versionFile)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(app.Run(ctx, os.Args[1:], app.Options{Version: version}))
}
