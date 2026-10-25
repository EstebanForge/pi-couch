# Pi Couch

A phone client for the pi coding agents running on my server.

I work from the phone the same way I work from the desk. I open the app over Tailscale and every pi session on the machine is right there, including the ones I started over ssh. I read the transcript live, I steer, I answer dialogs and the commit gate, I review diffs and leave line comments, I peek at the terminal when I have to.

Back at the desk I retake control over ssh, and with herdr, all my sessions are still in place.

Nothing gets locked into the UI. Sessions stay plain pi jsonl files, herdr stays the only driver, and the phone is just another client. If I delete the app tomorrow I lose a viewer, not my work.

| | |
|---|---|
| ![Home with priority sessions](docs/screenshots/home.jpg) | ![The speed dial](docs/screenshots/speed-dial.jpg) |
| ![The chat, working](docs/screenshots/chat.jpg) | ![The chat, answered](docs/screenshots/chat-answer.jpg) |

## Running it

The honest requirements: a Linux or macOS box (Windows WSL also works) with [pi](https://pi.dev) 1.x.x and [herdr](https://herdr.dev) 0.9.x on it, and a tunnel. I use [Tailscale](https://tailscale.com) and nothing else; it puts a private HTTPS address on the whole thing with zero config beyond the serve line below. herdr drives the sessions; Pi Couch speaks its socket protocol and nothing else.

The server binds 127.0.0.1 and trusts whatever sits in front of it. I front mine with `tailscale serve --bg --https=8443 8090` and set `origin` in the config to the public URL, so cross-site posts get rejected.

One suggestion from me: [herdrui.dev](https://herdrui.dev) is an optional desktop app for Windows, Linux and macOS, a nice integrated UI with the full terminal power. You install herdr anyway, so it's just a convenience on top.

### Installation

Releases carry compiled binaries, so most people just grab one from the [releases page](https://github.com/EstebanForge/pi-couch/releases) and skip the build. Building from source:

```sh
./scripts/build.sh              # builds the SPA, then the binary
bin/couch-pi --addr 127.0.0.1:8090
curl localhost:8090/healthz     # ok
```

Two wiring steps. First, the dialog bridge (pi loads extensions at agent start, so restart your agents after linking it):

```sh
ln -sf $PWD/extensions/couch-pi-bridge.ts ~/.pi/agent/extensions/couch-pi-bridge.ts
```

Second, [skydiver/herdr-pinned-workspaces](https://github.com/skydiver/herdr-pinned-workspaces), the one herdr plugin I run. herdr owns a workspace only while it holds a tab; close the last one and the workspace is gone. The plugin keeps your declared workspaces alive: recreated the moment they die and on every herdr start, so the set you build from the phone survives restarts and stray exits.

```sh
herdr plugin install skydiver/herdr-pinned-workspaces
```

Last, the service. `systemd/couch-pi.service` is a user unit on Linux: copy it to `~/.config/systemd/user/`, then `systemctl --user daemon-reload && systemctl --user enable --now couch-pi`.

For the phone, `android/` builds a thin WebView APK: `./gradlew assembleDebug`, debug-signed, sideload it. A mobile browser works fine too. The APK just behaves better with safe areas and a launcher icon.

## Configuring it

Machine settings live in one commented file, `~/.config/pi-couch/config.toml`. Copy `pi-couch.example.toml` there and edit. Every key is optional; run with no file and you get safe defaults. Command-line flags (`--addr`, `--origin`, `--sessions`) override the file when you just want to poke at something.

- `addr`: where the HTTP server listens. Keep it on loopback; the tunnel fronts TLS. Defaults to `127.0.0.1:8090`.
- `origin`: the public URL your phone uses, e.g. `https://yourbox.your-tailnet.ts.net:8443`. The cross-site guard accepts exactly this host, so set it, or state-changing requests only work from loopback.
- `sessions_dir`: where pi writes sessions. Defaults to `~/.pi/agent/sessions`, which is what you want.
- `uploads_dir`: where attachments land before their paths get pasted into messages. Defaults to `/tmp/couch-pi-uploads`; the pi agents on the same machine read them from there.
- `memory_url` and `memory_secret`: the agentmemory health probe behind the chip in the composer. Defaults to `localhost:3111`; set the URL if yours is elsewhere. Put the secret in the `AGENTMEMORY_SECRET` environment variable instead of the file, and keep the file 0600 either way.

The environment variables `AGENTMEMORY_URL` and `AGENTMEMORY_SECRET` override the config values; that's how I wire the secret into the systemd unit without it touching the repo.

## Security

There's no login and there's no token. The security model is the tunnel: the server only listens on loopback, and whatever can reach the tunneled address can drive your agents (read, steer, run commands, commit). Mine is Tailscale, so the address is reachable only inside my tailnet, and the tailnet is the trust boundary. That's the entire model.

Don't port-forward this to the public internet; that hands your machine to anyone who finds the port. If you need exposure beyond a private tunnel, add auth in front (a reverse proxy with basic auth works) before you blame the door you left open.

## Notes

Tested against pi 1.3.x and herdr 0.9.5. Dialog answering reads the pane paint, so a pi TUI reshuffle can break it. The rest of the client reads the session files, which move much slower.

State lives in `~/.local/state/couch-pi` (models cache, push subscriptions). No database, no accounts, one user. It's my setup, extracted and released as-is.

Inspired by the private phone setup Mario Zechner built for himself. Built on [pi](https://earendil.com) and herdr.

MIT, see [LICENSE](LICENSE).
