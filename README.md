# AIPilot CLI

[![Release](https://img.shields.io/github/v/release/softwarity/aipilot-cli)](https://github.com/softwarity/aipilot-cli/releases/latest)
[![License](https://img.shields.io/github/license/softwarity/aipilot-cli)](LICENSE)
[![CI](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml)

Bridge your terminal to the **AIPilot mobile app** via WebSocket relay. Control your AI coding agents (Claude Code, etc.) from your phone using voice input. No SSH required, no ports to open.

<a href="https://play.google.com/store/apps/details?id=com.softwarity.aipilot">
  <img alt="Get it on Google Play" src="https://play.google.com/intl/en_us/badges/static/images/badges/en_badge_web_generic.png" width="200"/>
</a>

## What is AIPilot?

AIPilot transforms your smartphone into a **voice remote control** for AI coding agents like Claude Code.

- **Voice Input**: Talk to your AI agent instead of typing
- **Hands-free Coding**: Keep coding from your couch, standing desk, or anywhere in the room
- **Real-time Output**: See AI responses on your phone as they stream

## How it works

```
┌─────────────────┐       WebSocket        ┌─────────────────┐
│                 │      via Relay         │                 │
│   AIPilot CLI   │◄─────────────────────►│  AIPilot App    │
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

### Quick install (Linux/macOS)

```bash
# Linux amd64
curl -L https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-amd64 -o aipilot-cli && chmod +x aipilot-cli

# Linux arm64
curl -L https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-arm64 -o aipilot-cli && chmod +x aipilot-cli

# macOS Intel
curl -L https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-amd64 -o aipilot-cli && chmod +x aipilot-cli

# macOS Apple Silicon
curl -L https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-arm64 -o aipilot-cli && chmod +x aipilot-cli
```

### Download binaries

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux    | amd64        | [aipilot-cli-linux-amd64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-amd64) |
| Linux    | arm64        | [aipilot-cli-linux-arm64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-linux-arm64) |
| macOS    | Intel        | [aipilot-cli-macos-amd64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-amd64) |
| macOS    | Apple Silicon| [aipilot-cli-macos-arm64](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-macos-arm64) |
| Windows  | amd64        | [aipilot-cli-windows-amd64.exe](https://github.com/softwarity/aipilot-cli/releases/latest/download/aipilot-cli-windows-amd64.exe) |

Or browse all releases: [GitHub Releases](https://github.com/softwarity/aipilot-cli/releases)

## Usage

```bash
# Default: runs 'claude' in current directory
aipilot-cli

# Custom command
aipilot-cli --command bash

# Specify working directory
aipilot-cli --workdir /path/to/project

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

## Build from source

```bash
git clone https://github.com/softwarity/aipilot-cli.git
cd aipilot-cli
go build -o aipilot-cli .
```

## Privacy & Security

- All connections are encrypted (WSS/TLS)
- No data stored on relay servers
- See our [Privacy Policy](PRIVACY_POLICY.md)

## License

MIT - see [LICENSE](LICENSE)

---

Made with ❤️ by [Softwarity](https://softwarity.io)
