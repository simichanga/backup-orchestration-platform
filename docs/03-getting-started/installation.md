# Installation

## Prerequisites

- **Go 1.25+** if building from source.
- **Restic** binary installed on the controller host (version 0.16.0+).
- **SSH access** from controller to target hosts (key‑based authentication).
- **One of the supported data sources** (PostgreSQL, Docker, etc.) on target hosts.

## Download Binary

Pre‑compiled binaries for Linux (amd64, arm64) are available on the GitHub
Releases page.

```bash
curl -LO https://github.com/example/bop/releases/latest/bop-linux-amd64
chmod +x bop-linux-amd64
sudo mv bop-linux-amd64 /usr/local/bin/bop
```

Docker
```bash
docker pull ghcr.io/example/bop:latest
docker run -v ./config:/etc/bop -v ./data:/var/lib/bop bop controller
```

Building from source
```bash
git clone https://github.com/example/bop.git
cd bop
make build
sudo cp bin/bop /usr/local/bin/
```

Core plugins (postgres, filesystem) are compiled into the `bop` binary - no
separate plugin install step is needed to follow the [Quickstart](quickstart.md).
The `plugins.dir` config key exists for future third-party/out-of-tree plugins.

Setting up a systemd service
```systemd
[Unit]
Description=Backup Orchestration Platform
After=network.target

[Service]
ExecStart=/usr/local/bin/bop controller --config /etc/bop/config.yaml
Restart=always
User=bop
Group=bop

[Install]
WantedBy=multi-user.target
```

Place this at `/etc/systemd/system/bop.service`, then:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bop
```

Verify installation
```bash
bop version
bop controller --help
```