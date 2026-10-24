# Pi Couch

A phone client for the pi coding agents running on my server.

I work from the phone the same way I work from the desk. I open the app over Tailscale and every pi session on the machine is right there, including the ones I started over ssh. I read the transcript live, I steer, I answer dialogs and the commit gate, I review diffs and leave line comments, I peek at the terminal when I have to.

Back at the desk I retake control over ssh in the same sessions.

Nothing gets locked into the UI. Sessions stay plain pi jsonl files, herdr stays the only driver, and the phone is just another client. If I delete the app tomorrow I lose a viewer, not my work.

## Running it

The honest requirements: a Linux or macOS box with pi 1.3.x and herdr 0.9.x on it, Go and node to build, and a tunnel you trust. herdr drives the sessions; Pi Couch speaks its socket protocol and nothing else.

```sh
./scripts/build.sh              # builds the SPA, then the binary
bin/couch-pi --addr 127.0.0.1:8090
curl localhost:8090/healthz     # ok
```

The server binds 127.0.0.1 and trusts whatever sits in front of it. There is no login screen. I front mine with `tailscale serve --bg --https=8443 8090` and pass `--origin` with the public URL, so cross-site posts get rejected. Do not port-forward it bare to the internet.

Two wiring steps. First, the dialog bridge (pi loads extensions at agent start, so restart your agents after linking it):

```sh
ln -sf $PWD/extensions/couch-pi-bridge.ts ~/.pi/agent/extensions/couch-pi-bridge.ts
```

Second, the service. `systemd/couch-pi.service` is a user unit on Linux: copy it to `~/.config/systemd/user/`, then `systemctl --user daemon-reload && systemctl --user enable --now couch-pi`.

For the phone, `android/` builds a thin WebView APK: `./gradlew assembleDebug`, debug-signed, sideload it. A mobile browser works fine too. The APK just behaves better with safe areas and a launcher icon.

## Notes

Tested against pi 1.3.x and herdr 0.9.5. Dialog answering reads the pane paint, so a pi TUI reshuffle can break it. The rest of the client reads the session files, which move much slower.

State lives in `~/.local/state/couch-pi` (models cache, push subscriptions). No database, no accounts, one user. It is my setup, extracted and released as-is.

Inspired by the private phone setup Mario Zechner built for himself. Built on [pi](https://earendil.com) and herdr.

MIT, see [LICENSE](LICENSE).
