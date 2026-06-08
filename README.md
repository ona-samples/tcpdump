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

When the services start, they automatically expose their ports with
`ona env port open`. Use the runner URLs from `ona env port list` for tests from
outside the environment:

```bash
ona env port list
```

Choose local URLs when testing inside the environment, or runner URLs when
testing through Ona's reverse proxy. Runner URLs use `https://` and `wss://`
because Ona adds TLS at the proxy.

| Service | Local value | Runner value |
| --- | --- | --- |
| HTTP | `curl --http2-prior-knowledge http://localhost:8080/` | `curl --http2 https://<8080-runner-domain>/` |
| WebSocket | `ws://localhost:8081/` | `wss://<8081-runner-domain>/` |
| SSH tunnel | `ws://localhost:8082/` | `wss://<8082-runner-domain>/` |
| SSH tunnel for `curl` | `http://localhost:8082/` | `https://<8082-runner-domain>/` |

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
WEBSOCKET_URL=ws://localhost:8081/

timeout 3 websocat "${WEBSOCKET_URL}"

wscat -c "${WEBSOCKET_URL}"
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

Test an SSH client through the WebSocket tunnel:

```bash
SSH_TUNNEL_URL=ws://localhost:8082/

ssh \
  -i ~/.ssh/ona/id_ed25519 \
  -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=no \
  -o "ProxyCommand=websocat -q --binary --protocol ssh -H='X-Gitpod-WebSocket-Tunnel: ssh' ${SSH_TUNNEL_URL}" \
  gitpod_devcontainer@network-troubleshoot
```

The SSH service auto-detects the local SSH target by probing the known Ona VM
SSH endpoints for an SSH banner. To force a different target while running the
tool directly, override `ONA_SSH_TARGET_ADDR`:

```bash
ONA_SSH_TARGET_ADDR=127.0.0.1:22999 go run ./tools/network-troubleshoot --mode ssh --addr 0.0.0.0:8082
```

The service stop commands close the matching exposed ports.

### Traffic capture

Start the tcpdump capture service:

```bash
gitpod automations service start tcpdump
```

The service captures traffic on the known VM SSH endpoint ports and the
troubleshooting service ports:

- `22222`
- `22999`
- `29222`
- `8080`
- `8081`
- `8082`

Captures are written to timestamped files under `captures/`:

```text
captures/tcpdump-YYYYMMDDTHHMMSSZ.pcap
```

Check service status:

```bash
gitpod automations service list
```

Stop the capture service:

```bash
gitpod automations service stop tcpdump
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
