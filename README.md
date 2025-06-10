# PwnPing

A real-time network monitoring tool that tracks packet loss and latency to help diagnose internet connectivity issues. I wrote this because my internet has been randomly cutting out, and it's been driving me crazy. Now I finally have proof that the problem isn't on my end – just wish the ISP would take it seriously.

## Features

- **Real-time monitoring** of multiple hosts simultaneously (Gateway, Google DNS, Cloudflare DNS)
- **Visual graphs** showing latency trends over time
- **Latency distribution histograms** to identify patterns
- **Packet loss tracking** with immediate visual feedback
- **Detailed logging** for evidence collection and later analysis
- **Web-based log analyzer** for identifying network events and outages
- **Configurable router/gateway IP** to work with any network setup

## Why PwnPing?

When your internet keeps dropping and your ISP insists "everything looks fine from our end", you need hard data. PwnPing continuously monitors your connection to:

- Your local gateway (to rule out local network issues)
- External DNS servers (to confirm internet connectivity problems)

The tool clearly shows when external connections fail while your local network remains stable – proving the issue is with your ISP, not your equipment.

## Installation

### Prerequisites

- Go
- Root/Administrator privileges (required for ICMP ping)

### Build from source

```bash
# Clone the repository
git clone https://github.com/yourusername/pwnping.git
cd pwnping

# Download dependencies
make deps

# Build for your current platform
make build

# Or build for all platforms
make build-all
```

## Usage

### Basic usage

```bash
# Run with default settings (monitors 192.168.1.1, 8.8.8.8, 1.1.1.1)
sudo ./pwnping

# Or using make
make run
```

### Custom router IP

```bash
# Specify your router's IP address
sudo ./pwnping -router=10.0.0.1

# Or using make
make run-custom ROUTER=10.0.0.1
```

### Controls

- Press `q` to quit
- The display automatically resizes with your terminal

## Understanding the Display

### Latency Categories

- **Great**: <15ms (typical for local/nearby servers)
- **Good**: 15-50ms (normal for most internet services)
- **Bad**: 50-200ms (noticeable lag, poor for gaming/video calls)
- **Unusable**: >200ms (severe issues)

## Log Files

PwnPing automatically creates detailed logs in the `logs/` directory:

- `pwnping_YYYYMMDD_HHMMSS.log` - Main application log
- `<IP>_YYYYMMDD_HHMMSS.log` - Per-host detailed ping logs

These logs are invaluable when disputing with your ISP.

## Troubleshooting

### Permission Denied

ICMP ping requires root privileges:

```bash
# Always run with sudo
sudo ./pwnping
```

### No hosts could be monitored

Check your network configuration and ensure the hosts are reachable:

```bash
# Test connectivity first
ping 8.8.8.8
ping 1.1.1.1
```

### Custom networks

If your router uses a different IP (e.g., 10.0.0.1, 192.168.0.1):

```bash
sudo ./pwnping -router=YOUR_ROUTER_IP
```

## Building for Different Platforms

```bash
# Linux
make linux-amd64
make linux-arm64

# macOS
make darwin-amd64
make darwin-arm64

# Windows
make windows-amd64
make windows-arm64
```

## Analyzing Logs

Use the included web-based analyzer to visualize your logs:

1. Open `ping-analyzer.html` in your browser
2. Upload up to 3 log files
3. View synchronized graphs showing:
   - Latency over time
   - Packet loss percentages
   - Network events and outages

The analyzer makes it easy to see patterns like:

- Internet connections failing while local gateway remains stable
- Periodic outages at specific times
- Gradual degradation vs sudden failures

## Contributing

Contributions are welcome! Whether it's:

- Adding new monitoring targets
- Improving the visualization
- Enhancing the log analyzer

Please open an issue first to discuss major changes.

## License

MIT License

## Acknowledgments

- Built with [termui](https://github.com/gizak/termui) for the terminal interface
- Uses [pro-bing](https://github.com/prometheus-community/pro-bing) for ICMP operations
- Vibe-coded with [claude cli](claude.ai/code)
- Inspired by countless hours of internet outages and ISP support calls

---

_Remember: When your ISP says "Have you tried turning it off and on again?" you can now respond with "Have you tried looking at my packet loss graphs?"_
