# tcpdump

This repository configures an Ona devcontainer for capturing SSH traffic on the
environment VM host. The devcontainer runs in privileged mode with host
networking and includes `tcpdump`, `tshark`, `capinfos`, `termshark`, and VS Code
pcap viewing extensions.

## Usage

### Network troubleshooting services

The repository defines three manual Ona services that expose plain HTTP ports.
Do not configure HTTPS in the services themselves; Ona adds HTTPS when it exposes
ports through its reverse proxy.

| Service | Port | Purpose |
| --- | ---: | --- |
| `http` | `8080` | Starts a cleartext HTTP/2 server that streams a UTC timestamp once per second. |
| `websocket` | `8081` | Starts a plain HTTP WebSocket server that sends a UTC timestamp once per second. |
| `ssh` | `8082` | Starts a plain HTTP WebSocket SSH tunnel that mirrors Ona's `ssh` tunnel headers and forwards to the VM SSH server. |

Start a service with:

```bash
gitpod automations service start http
gitpod automations service start websocket
gitpod automations service start ssh
```

Test the HTTP/2 timestamp stream:

```bash
curl --http2-prior-knowledge http://localhost:8080/
```

Expected output is one line per second:

```text
2026-06-08T13:45:33.681453425Z HTTP/2.0
2026-06-08T13:45:34.681544383Z HTTP/2.0
```

Test the WebSocket timestamp stream:

```bash
timeout 3 websocat ws://localhost:8081/
```

Or interactively with `wscat`:

```bash
wscat -c ws://localhost:8081/
```

Expected output is one decoded WebSocket message per second:

```text
2026-06-08T13:46:06.2348558Z
2026-06-08T13:46:07.234948987Z
```

`wscat` prints the same messages with an interactive prompt and a `<` prefix.
Press `Ctrl+C` to stop it.

Test the SSH-over-WebSocket tunnel:

```bash
curl -i --http1.1 --max-time 3 \
  -H 'Connection: Upgrade' \
  -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  -H 'Sec-WebSocket-Protocol: ssh' \
  -H 'X-Gitpod-WebSocket-Tunnel: ssh' \
  http://localhost:8082/
```

Expected output starts with `HTTP/1.1 101 Switching Protocols`, includes
`Sec-WebSocket-Protocol: ssh`, and then returns the SSH server banner inside a
WebSocket binary frame, for example `SSH-2.0-OpenSSH_...`.

The SSH service forwards to `127.0.0.1:22222` by default. To test a different
target while running the tool directly, override `ONA_SSH_TARGET_ADDR`:

```bash
ONA_SSH_TARGET_ADDR=127.0.0.1:22999 go run ./tools/network-troubleshoot --mode ssh --addr 0.0.0.0:8082
```

When these services start, they automatically open their ports through Ona with
`--protocol http`. Their stop commands close the matching exposed ports.

### SSH traffic capture

Start the SSH capture service:

```bash
gitpod automations service start capture-ssh-traffic
```

The service captures traffic on the known VM SSH endpoint ports:

- `22222`
- `22999`
- `29222`

Captures are written to timestamped files under `captures/`:

```text
captures/ssh-YYYYMMDDTHHMMSSZ.pcap
```

Check service status:

```bash
gitpod automations service list
```

Stop the capture service:

```bash
gitpod automations service stop capture-ssh-traffic
```

Stopping the service sends `SIGINT` to `tcpdump`, allowing it to flush and close
the pcap file cleanly. If `tcpdump` does not exit within 30 seconds, the service
falls back to `SIGTERM`.

Inspect a capture from the terminal:

```bash
tshark -r captures/<file>.pcap
capinfos captures/<file>.pcap
termshark -r captures/<file>.pcap
```

You can also open `.pcap` files in VS Code using the configured pcap viewer
extension. Generated `.pcap` files are ignored by git.
