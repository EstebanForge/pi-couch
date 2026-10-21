package main

import (
	"context"
	"flag"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/push"
	"github.com/EstebanForge/couch-pi/internal/server"
	"github.com/EstebanForge/couch-pi/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address; bind loopback only, tailscale serve fronts TLS")
	sessions := flag.String("sessions", "", "pi sessions root (default ~/.pi/agent/sessions)")
	origin := flag.String("origin", "", "public https origin (e.g. https://<host>.ts.net); cross-site POST guard allowlist")
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
	// SIGTERM is what systemd sends on stop; without it the process just dies.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	idx := pi.NewIndex(root)
	watcher, err := pi.StartWatcher(ctx, root)
	if err != nil {
		logger.Warn("live updates off", "err", err) // heartbeat still refreshes
		watcher = nil
	}

	hc := herdr.New()

	st, err := store.New()
	if err != nil {
		logger.Error("state dir", "err", err)
		os.Exit(1)
	}

	// P5 web push: VAPID identity on first run, then one agent.wait watcher
	// per live agent (ADR-0005) pushing stable transitions to subscribers.
	vapid, err := st.VAPID()
	if err != nil {
		logger.Error("vapid keys", "err", err)
		os.Exit(1)
	}
	pusher := &push.Sender{
		Public:     vapid.Public,
		Private:    vapid.Private,
		Subscriber: "mailto:couch-pi@localhost",
		TTL:        3600,
		Timeout:    10 * time.Second,
	}
	onEvent := notifyFunc(logger, idx, st, pusher)
	go (&push.Watcher{HC: hc, Reconcile: 15 * time.Second, Stability: 2 * time.Second, WaitMax: 15 * time.Minute, Rearm: 5 * time.Second, OnEvent: onEvent, Log: logger}).Run(ctx)

	if err := server.Run(ctx, logger, *addr, idx, hc, watcher, st, pusher, *origin); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// notifyFunc pushes one transition to every subscription and drops the
// endpoints that answered 410 Gone.
func notifyFunc(logger *slog.Logger, idx *pi.Index, st *store.Store, pusher *push.Sender) func(push.Event) {
	return func(e push.Event) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		name := e.Name
		link := "/"
		if meta, ok := idx.ByPath(e.Path); ok {
			if meta.Name != "" {
				name = meta.Name
			} else if meta.FirstUser != "" {
				name = meta.FirstUser
			}
			link = "/sessions/" + url.PathEscape(meta.Key)
		}
		body := map[string]string{
			"done":    "Agent completed task execution.",
			"blocked": "Agent is blocked and requires input.",
			"idle":    "Agent is idle.",
		}[e.Status]
		subs, err := st.Subs()
		if err != nil {
			logger.Error("push subs", "err", err)
			return
		}
		if len(subs) == 0 {
			return
		}
		dead, err := pusher.Send(ctx, subs, push.Message{
			Title: "Pi Couch: " + name,
			Body:  body,
			URL:   link,
		})
		if err != nil {
			logger.Warn("push send", "err", err)
		}
		if len(dead) > 0 {
			deadSet := make(map[string]bool, len(dead))
			for _, d := range dead {
				deadSet[d] = true
			}
			err := st.UpdateSubs(func(subs []store.Sub) []store.Sub {
				remaining := subs[:0]
				for _, s := range subs {
					if !deadSet[s.Endpoint] {
						remaining = append(remaining, s)
					}
				}
				return remaining
			})
			if err != nil {
				logger.Error("push cleanup", "err", err)
			}
		}
	}
}
