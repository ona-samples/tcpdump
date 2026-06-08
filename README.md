# tcpdump

This repository configures an Ona devcontainer for capturing SSH traffic on the
environment VM host. The devcontainer runs in privileged mode with host
networking and includes `tcpdump`, `tshark`, `capinfos`, `termshark`, and VS Code
pcap viewing extensions.

## Usage

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
