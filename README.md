# AIPilot CLI

[![Release](https://img.shields.io/github/v/release/softwarity/aipilot-cli)](https://github.com/softwarity/aipilot-cli/releases/latest)
[![CI](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml)

Bridge your terminal to the **AIPilot mobile app** via WebSocket relay. Control your AI coding agents (Claude Code, etc.) from your phone using voice input. No SSH required, no ports to open.

<a href="https://play.google.com/store/apps/details?id=com.softwarity.aipilot">
  <img alt="Get it on Google Play" src="https://play.google.com/intl/en_us/badges/static/images/badges/en_badge_web_generic.png" width="200"/>
</a>

## Open Source & Privacy

**This CLI is fully open source.** You can inspect, audit, and build it yourself.

- **Source code**: Available right here on GitHub
- **No telemetry**: We don't collect any usage data or analytics
- **No tracking**: No user behavior tracking whatsoever
- **No accounts**: No registration or sign-up required
- **Encrypted**: All communications use TLS/WSS encryption
- **Ephemeral relay**: The relay server only forwards encrypted messages in real-time, no data is stored

The relay simply acts as a bridge between your PC and phone. Your terminal data passes through encrypted and is never logged or stored.

## What is AIPilot?

AIPilot transforms your smartphone into a **voice remote control** for AI coding agents like Claude Code.

- **Voice Input**: Talk to your AI agent instead of typing
- **Hands-free Coding**: Keep coding from your couch, standing desk, or anywhere in the room
- **Real-time Output**: See AI responses on your phone as they stream

## How it works

```
┌─────────────────┐       WebSocket        ┌─────────────────┐
│                 │      via Relay         │                 │
│   AIPilot CLI   │◄──────────────────────►│  AIPilot App    │
│   (Your PC)     │                        │  (Your Phone)   │
│                 │                        │                 │
└────────┬────────┘                        └─────────────────┘
         │                                   📱 Voice Input
         │ Spawns                            📱 Commands
         ▼                                   📱 File Sharing
┌─────────────────┐
│   AI Agent      │
│  (Claude Code)  │
└─────────────────┘
```

1. **Run the CLI** on your PC - it displays a QR code
2. **Scan the QR code** with the AIPilot mobile app
3. **Talk to your AI agent** using voice input
4. **See responses** streaming in real-time on your phone

All communication goes through a secure relay - no need to open ports or configure your firewall.

## Installation

### Quick install (recommended)

The installer downloads the latest release and installs it to your user directory. **No sudo or admin rights required.**

**Linux / macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/softwarity/aipilot-cli/main/install.sh | bash
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/softwarity/aipilot-cli/main/install.ps1 | iex
```

The installer:
- Detects your OS and architecture automatically
- Downloads the latest release from GitHub
- Installs to `~/.local/bin` (Linux/macOS) or `%LOCALAPPDATA%\Programs\aipilot` (Windows)
- Adds to PATH if needed

### Manual download

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux    | amd64        | [aipilot-cli-linux-amd64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-amd64) |
| Linux    | arm64        | [aipilot-cli-linux-arm64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-arm64) |
| macOS    | Intel        | [aipilot-cli-macos-amd64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-amd64) |
| macOS    | Apple Silicon| [aipilot-cli-macos-arm64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-arm64) |
| Windows  | amd64        | [aipilot-cli-windows-amd64.exe](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-windows-amd64.exe) |

After downloading, make it executable (Linux/macOS):
```bash
chmod +x aipilot-cli-*
mv aipilot-cli-* ~/.local/bin/aipilot-cli
```

### Build from source

```bash
git clone https://github.com/softwarity/aipilot-cli.git
cd aipilot-cli
go build -o aipilot-cli .
```

## Usage

```bash
# Default: runs 'claude' in current directory
aipilot-cli

# Specify working directory
aipilot-cli --workdir /path/to/project

# Custom command
aipilot-cli --command bash

# Custom relay (self-hosted)
aipilot-cli --relay wss://your-relay.example.com/ws
```

## Mobile App Features

The AIPilot mobile app provides:

- **Voice Recognition**: Dictate commands instead of typing
- **Multi-sessions**: Manage multiple projects simultaneously
- **Full Terminal**: Access all Claude commands (`/compact`, `/resume`, `/clear`...)
- **File Sharing**: Share photos and documents with your agent
- **Session History**: Quickly reconnect to previous sessions
- **SSH Mode**: Connect directly to remote servers (Pro feature)

### Free vs Pro

| Feature | Free | Pro |
|---------|------|-----|
| CLI connections | 1 | Unlimited |
| SSH connections | - | Unlimited |
| Agents | - | Unlimited |
| File upload | - | ✓ |

## License

MIT - see [LICENSE](LICENSE)

---

Made with ❤️ by [Softwarity](https://softwarity.io)
