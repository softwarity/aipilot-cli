# AIPilot CLI

[![Release](https://img.shields.io/github/v/release/softwarity/aipilot-cli)](https://github.com/softwarity/aipilot-cli/releases/latest)
[![License](https://img.shields.io/github/license/softwarity/aipilot-cli)](LICENSE)
[![CI](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/softwarity/aipilot-cli/actions/workflows/ci.yml)

Bridge your terminal to the AIPilot mobile app via WebSocket relay. No SSH required, no ports to open.

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

## How it works

1. **Run the CLI** - It displays a QR code in your terminal
2. **Scan with AIPilot app** - Open AIPilot on your phone and scan the QR code
3. **Connected!** - Your phone is now a remote terminal to your AI agent

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Mobile    │◄────►│   Relay     │◄────►│    CLI      │
│   AIPilot   │      │  (Cloud)    │      │   + PTY     │
└─────────────┘      └─────────────┘      └─────────────┘
     Phone            WebSocket            Your Computer
```

## Build from source

```bash
git clone https://github.com/softwarity/aipilot-cli.git
cd aipilot-cli
go build -o aipilot-cli .
```

## License

MIT
