package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/EstebanForge/piremote/internal/pi"
	"github.com/EstebanForge/piremote/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address; bind loopback only, tailscale serve fronts TLS")
	sessions := flag.String("sessions", "", "pi sessions root (default ~/.pi/agent/sessions)")
	flag.Parse()

	root := *sessions
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("no home dir", "err", err)
			os.Exit(1)
		}
		root = filepath.Join(home, ".pi", "agent", "sessions")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	idx := pi.NewIndex(root)
	if err := server.Run(logger, *addr, idx); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}
