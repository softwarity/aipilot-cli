# AIPilot CLI

Bridge your terminal to the AIPilot mobile app via WebSocket relay. No SSH required, no ports to open.

## Installation

Download the latest release for your platform from [GitHub Releases](https://github.com/softwarity/aipilot-cli/releases).

### Linux / macOS
```bash
chmod +x aipilot-cli-*
./aipilot-cli-linux-amd64  # or macos-amd64, macos-arm64
```

### Windows
```powershell
.\aipilot-cli-windows-amd64.exe
```

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
