# DockUP

![Docker](https://img.shields.io/badge/Docker-Manager%20%2B%20Updater-2496ED?logo=docker&logoColor=white)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)
![Version](https://img.shields.io/github/v/release/shui1iao/DockUP?label=Release)
![GHCR](https://img.shields.io/badge/GHCR-dockup-blue)

[中文](README.md) | **English**

**DockUP is a lightweight Telegram-based Docker manager with automatic update notifications and manual project controls.**

> By default, DockUP checks all running containers every 12 hours. You can also open the Telegram project panel to view status, manually check updates, start/stop/restart, or delete with confirmation.

---

## 🎯 Features

- **Telegram Docker dashboard**: view container / Compose project status and start, stop, restart, or confirm deletion.
- **Update notifications**: periodically check image updates and update only after Telegram confirmation.
- **Manual checks and batch updates**: check all projects, a single project, or projects on remote servers.
- **Multi-VPS management**: use a central server with remote Agents that connect back to it.
- **Safer operations**: Telegram chat authorization and config backups before removing remote server records.

DockUP does not provide a web dashboard and does not delete Docker volumes. Updates are applied only after confirmation in Telegram.

---

## 🚀 Quick Start

### 1. Create a Telegram Bot

1. Talk to [@BotFather](https://t.me/BotFather) and create a bot with `/newbot` to get `TG_BOT_TOKEN`
2. Get your `TG_CHAT_ID`: send a message to the bot and call `https://api.telegram.org/bot<TOKEN>/getUpdates`, or use any Telegram ID helper bot
3. Make sure the bot can send messages to your target chat

### 2. Deploy with Docker Compose

Docker Compose is recommended:

```bash
mkdir -p /opt/dockup && cd /opt/dockup
curl -Lo docker-compose.yml https://github.com/shui1iao/DockUP/releases/latest/download/docker-compose.yml
cat > .env <<'ENV'
TZ=Asia/Shanghai
TG_BOT_TOKEN=your Telegram bot token
TG_CHAT_ID=your Telegram chat id
CHECK_INTERVAL=12h
CLEANUP=true
SETUP_TEST_MESSAGE=true
ENV

docker compose pull
docker compose up -d
docker compose logs -f
```

### 3. Custom Compose

Or write your own compose file:

```yaml
services:
  dockup:
    image: ghcr.io/shui1iao/dockup:latest
    container_name: dockup
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      TG_BOT_TOKEN: your_bot_token
      TG_CHAT_ID: your_chat_id
      CHECK_INTERVAL: 12h
      CLEANUP: "true"
      SETUP_TEST_MESSAGE: "true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

---

## 🌐 Remote VPS Management

DockUP supports a **server + agent** mode:

- **server**: the Telegram control plane for menus, notifications, and operations
- **agent**: installed on each VPS and only manages local Docker / Compose


### Add servers from Telegram

After the first deployment, DockUP manages only the local host by default. Open the Telegram main menu and click **➕ Add Server**:

1. Send a server name; or send the server IP directly and it will be used as the default name
2. The bot will generate a Docker Compose install command
3. Run the command on the target server; the Agent will actively connect back to the server
4. The bot will notify when pairing succeeds, and the host appears under **🌐 Remote VPS**

Paired servers are persisted in `DOCKUP_DATA`, default `/data/dockup.json`.

### Remove a remote VPS

Open **🌐 Remote VPS**, enter the server detail page, and click **🗑 Delete Server**.

DockUP first shows the remote uninstall command:

```bash
cd /opt/dockup && docker compose down
```

If you are sure `/opt/dockup` has no content you want to keep, you may clean the directory manually:

```bash
rm -rf /opt/dockup
```

After running the command, return to the bot and click **I have run it, check and delete**. DockUP checks that the Agent is offline, then backs up and removes the local server record. If the remote machine is already gone, you can use **force remove local record only**.

Before removing a server record, DockUP automatically backs up `DOCKUP_DATA` under the sibling `backups/` directory, for example:

```text
/data/backups/dockup.json.20260523-114500.bak
```

### Optional static remote VPS configuration

Telegram pairing is the recommended path. You can also preconfigure remote Agents in the server `.env`:

```env
DOCKUP_MODE=server
DOCKUP_PUBLIC_URL=http://your-server-ip-or-domain:8748
DOCKUP_AGENTS=vps1||VPS-1|one_token,vps2||VPS-2|another_token
```

`DOCKUP_AGENTS` format:

```text
id|agent_url|display_name|token
```

For active reverse Agent mode, `agent_url` may be empty. The legacy passive HTTP Agent mode remains compatible when a URL is provided. Separate multiple Agents with commas.

---

## ⚙️ Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `TG_BOT_TOKEN` | empty | Telegram Bot Token; notifications are disabled if empty |
| `TG_CHAT_ID` | empty | Telegram Chat ID; notifications are disabled if empty; commands and button callbacks are also authorized against this ID |
| `CHECK_INTERVAL` | `12h` | Check interval, Go duration format such as `30m`, `1h`, `12h`; set to `0` to disable automatic update checks |
| `CHECK_LOCAL` | `true` | Whether the server checks local Docker containers; useful for remote-only or notification tests |
| `TZ` | `Asia/Shanghai` | Time zone |
| `CLEANUP` | `true` | Try to remove old images after approved successful updates |
| `RUN_ONCE` | `false` | Run one check and exit |
| `UPDATE_TIMEOUT` | `10m` | Timeout for one update pass |
| `DOCKUP_MODE` | `server` | Run mode: `server` or `agent` |
| `DOCKUP_AGENT_TOKEN` | empty | Agent Bearer Token; Telegram pairing generates one token per server automatically |
| `DOCKUP_PUBLIC_URL` | empty | Public server URL for reverse Agents, for example `http://1.2.3.4:8748` |
| `DOCKUP_AGENTS` | empty | Optional static Agent list, format: `id|url|display_name|token`; `url` may be empty for reverse mode |
| `AGENT_LISTEN` | `:8748` | Server reverse hub / legacy Agent HTTP API listen address |
| `AGENT_PORT` | `8748` | Docker Compose host port for the server reverse hub |
| `DOCKUP_DATA` | `/data/dockup.json` | Persistent server list and pairing state |
| `SETUP_TEST_MESSAGE` | `true` | Send an entry message after start, restart, or update |

---

## 💬 Telegram Interaction

After every start, restart, or update, DockUP sends an entry message for the Docker management panel. When an update is found, DockUP sends one Telegram message per container with two buttons: `Update` and `Ignore`. No-update runs are logged only.

Manual **Check All Updates** performs a fresh scan of all local containers and online remote Agents. Previous `Ignore` clicks or pending notifications do not affect this manual result. The result page shows local / remote update counts and provides both `Update all` and per-item update buttons.

The command menu is registered automatically:

| Command | Description |
| --- | --- |
| `/start` | Open the DockUP main menu |
| `/docker` | View Docker / Compose projects |
| `/checkall` | Check all containers immediately |
| `/settings` | Set the automatic check interval |
| `/help` | Show help and the main menu |

The management panel supports:

- Listing Docker / Compose projects
- Viewing project status, images, image version, ports, CPU, memory, network, and block I/O
- Automatically registers Telegram menu commands: `/start`, `/docker`, `/settings`, and `/checkall`
- Manually checking all containers, one remote VPS, or one project, then updating individually or in batch
- Starting, stopping, and restarting projects
- Delete confirmation before removing containers
- Remote VPS removal with a fixed uninstall command and local record deletion after the Agent is offline
- Temporarily changing the automatic check interval

---

## 🛠 How It Works

For each automatic check, DockUP:

1. Lists all running containers
2. Pulls the current `image:tag` used by each container
3. Compares the running container image ID with the pulled target tag image ID, so pulling `latest` ahead of time does not hide pending updates
4. Tries to resolve a semantic version from image labels or registry digests; falls back to a short image ID when unavailable
5. If the image changed:
   - Sends a per-container Telegram message with buttons, preferring versions like `vX.Y.Z` and falling back to short image IDs
   - After `Update` is clicked, stops the old container
   - Renames it as a backup container
   - Creates a new container with the original configuration
   - Starts the new container
   - Waits for health checks to become healthy
   - Removes the backup container after success
   - Optionally removes the old image
6. If the new container fails to start or becomes unhealthy, DockUP attempts to remove it and restore the old container

Remote Agents connect back to the server through a reverse connection. For normal remote containers, the server waits for the Agent result. When the target is the remote `dockup-agent` container itself, the Agent starts a temporary helper for the self-update and returns immediately, avoiding a mid-request disconnect that would leave Telegram stuck on “updating”.

---

## ⚠️ Notes

DockUP is intentionally direct: if an update is found, it asks for confirmation in Telegram.

That means:

- A bad upstream image may still break your service if you approve the update
- Core services such as databases, reverse proxies, and dashboards should be reviewed before clicking Update
- Mounting `/var/run/docker.sock` gives DockUP Docker management access on the host
- With `latest` images, DockUP decides updates by the running container's actual image ID, not merely whether the local tag has already been pulled
- Telegram commands and button callbacks are accepted only from the configured `TG_CHAT_ID`
- Removing a remote VPS does not execute arbitrary remote commands; DockUP only shows a fixed uninstall command and the user runs it on the remote host

If you need allowlists, complex approval workflows, multiple notification channels, or orchestration, DockUP is not the right tool. It is designed to stay small and simple as a Telegram Docker manager + update notifier.

---

## 📦 Image

```bash
docker pull ghcr.io/shui1iao/dockup:latest
docker pull ghcr.io/shui1iao/dockup:<version>
```

Supported platforms:

- `linux/amd64`
- `linux/arm64`

---

## 🔐 Privacy

DockUP does not upload your container list or configuration. Network requests are limited to:

- Pulling images from Docker registries to detect updates
- Sending button notifications, registering bot commands, and receiving button/command callbacks through the Telegram Bot API


---

## 📄 License

MIT License
