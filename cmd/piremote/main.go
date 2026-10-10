package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/EstebanForge/piremote/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address; bind loopback only, tailscale serve fronts TLS")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := server.Run(logger, *addr); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}
