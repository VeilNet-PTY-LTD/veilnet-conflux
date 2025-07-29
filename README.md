# VeilNet Conflux

A lightweight software that connects to the VeilNet network through secure TUN interfaces. The VeilNet Conflux establishes encrypted connections to VeilNet, enabling secure, private networking for your applications and devices.

By running the VeilNet Conflux, you can access the decentralized VeilNet network, bypass network restrictions, and maintain privacy while browsing the internet (Rift Mode, requires at least one peer portal if in private plane).

> **⚠️ Warning**: Darwin (macOS) support is experimental and has not been fully tested. Use at your own risk.

## Features

- **Secure TUN Interface**: Creates a virtual network interface for encrypted traffic
- **Privacy-First**: All traffic is encrypted and routed through the VeilNet network
- **Cross-Platform**: Support for Linux, macOS, Windows, and ARM architectures
- **Easy Configuration**: Simple command-line interface with environment variable support
- **Graceful Shutdown**: Proper cleanup of network interfaces and routes
- **Docker Support**: Containerized deployment with Docker and Docker Compose
- **Portal Mode**: Support for both client and portal modes
- **Conflux Management**: Register and unregister conflux instances through the CLI

## Prerequisites

Before setting up your VeilNet Conflux, ensure you have:

- **Operating System**: Linux, macOS, or Windows
- **Root/Admin Access**: Required for TUN device creation and network configuration
- **Network Connectivity**: Stable internet connection
- **Guardian Account**: Access to the VeilNet Guardian service
- **Conflux Token**: A valid conflux token from the Guardian service

> **Note**: macOS (Darwin) support is experimental and may require additional setup or troubleshooting.

## Quick Start

### 1. Get Your Conflux Token

1. Visit [console.veilnet.org](https://console.veilnet.org)
2. Sign in or create an account
3. Navigate to the Conflux section
4. Create a Conflux and obtain your token
5. Note down your token for configuration

### 2. Choose Your Deployment Method

#### Option A: Docker (Recommended)

**Using Docker Compose:**

1. **Create docker-compose.yml**:
```yaml
services:
  veilnet-conflux:
    build: .
    container_name: veilnet-conflux
    image: veilnet/conflux:latest
    pull_policy: always
    restart: unless-stopped
    # use this for Rift mode so that the host will use VeilNet as internet access, only available on Linux.
    # network_mode: host 
    privileged: true
    env_file:
      - .env
```

2. **Create .env file**:
```bash
VEILNET_TOKEN=your-conflux-token-here
VEILNET_PORTAL=false # or true
```

3. **Run**:
```bash
docker-compose up -d
```

**Using Docker directly:**
```bash
docker run -d \
  --name veilnet-conflux \
  --privileged \
  -e VEILNET_TOKEN=your-conflux-token \
  -e VEILNET_PORTAL=false \
  veilnet/conflux:latest
```

#### Option B: Native Installation

1. **Download the binary**:
Download the binary from the releases page.

2. **Make it executable**:
```bash
chmod +x veilnet-conflux-*
# Linux   amd64 veilnet-conflux
# Linux   arm64 veilnet-conflux-arm64
# Windows amd64 veilnet-conflux.exe
# Windows arm64 veilnet-conflux-arm64.exe
# Darwin  amd64 veilnet-conflux-darwin-amd64
# Darwin  arm64 veilnet-conflux-darwin-arm64
```

3. **Run the conflux**:
```bash
# Basic usage
sudo ./veilnet-conflux up \
  -t your-conflux-token

# With portal mode enabled
sudo ./veilnet-conflux up \
  -t your-conflux-token \
  -p

# Or using environment variables
export VEILNET_TOKEN="your-conflux-token"
export VEILNET_PORTAL="false"

sudo ./veilnet-conflux up
```

### 3. Verify Your Connection

1. **Check network interface**: The conflux creates a `veilnet` TUN interface
2. **Monitor logs**: Check the application logs for connection status
3. **Test connectivity**: Verify your traffic is being routed through VeilNet

## Configuration

### Command Line Options

The VeilNet Conflux supports multiple commands:

#### `up` Command - Start the Conflux

| Option | Flag | Description | Required | Default |
|--------|------|-------------|----------|---------|
| Token | `-t, --token` | Your conflux authentication token | Yes | - |
| Portal | `-p, --portal` | Enable portal mode | No | `false` |
| Guardian | `-g, --guardian` | The Guardian URL (Authentication Server) | No | `https://guardian.veilnet.org` |

#### `register` Command - Register a New Conflux

| Option | Flag | Description | Required |
|--------|------|-------------|----------|
| Email | `--email` | The email to login with VeilNet Guardian | Yes |
| Password | `--password` | The password to login with VeilNet Guardian | Yes |
| Name | `--name` | The name of the conflux | Yes |
| Plane | `--plane` | The plane to register on | Yes |
| Tag | `--tag` | The tag for the conflux | Yes |

#### `unregister` Command - Unregister a Conflux

| Option | Flag | Description | Required |
|--------|------|-------------|----------|
| Email | `--email` | The email to login with VeilNet Guardian | Yes |
| Password | `--password` | The password to login with VeilNet Guardian | Yes |
| Name | `--name` | The name of the conflux | Yes |
| Plane | `--plane` | The plane to register on | Yes |

### Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `VEILNET_TOKEN` | Your conflux authentication token | Yes | - |
| `VEILNET_PORTAL` | Enable portal mode | No | `false` |
| `VEILNET_GUARDIAN_URL` | The Guardian URL (Authentication Server) | No | `https://guardian.veilnet.org` |

### Configuration Priority

Configuration values are loaded in this order (later overrides earlier):

1. **Default values** (hardcoded defaults)
2. **Environment variables** (with `VEILNET_` prefix)
3. **Command line flags** (highest priority)

## Usage Examples

### Basic Connection
```bash
sudo ./veilnet-conflux up \
  -t eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### Portal Mode
```bash
sudo ./veilnet-conflux up \
  -t your-conflux-token \
  -p
```

### Register a New Conflux
```bash
./veilnet-conflux register \
  --email your-email@example.com \
  --password your-password \
  --name my-conflux \
  --plane default \
  --tag production
```

### Unregister a Conflux
```bash
./veilnet-conflux unregister \
  --email your-email@example.com \
  --password your-password \
  --name my-conflux \
  --plane default
```

### Using Environment Variables
```bash
export VEILNET_TOKEN="your-token"
export VEILNET_PORTAL="false"

sudo ./veilnet-conflux up
```

### Docker with Custom Configuration
```bash
docker run -d \
  --name veilnet-conflux \
  --privileged \
  -e VEILNET_TOKEN="your-token" \
  -e VEILNET_PORTAL="false" \
  veilnet/conflux:latest up
```

## Network Configuration

The VeilNet Conflux automatically configures your network:

1. **Creates TUN Interface**: Establishes a virtual network interface named `veilnet`
2. **Configures Routes**: Sets up routing to direct traffic through the VeilNet network
3. **Bypass Routes**: Adds routes for Cloudflare STUN/TURN servers to maintain connectivity
4. **Cleanup**: Properly removes all network changes on shutdown

### Network Interface Details

- **Interface Name**: `veilnet`
- **Type**: TUN (Layer 3)
- **MTU**: 1500
- **IP Assignment**: Dynamic from Guardian service

### Portal Mode vs Rift Mode

- **Rift Mode** (default): Routes all traffic through the VeilNet network
- **Portal Mode** (`-p` flag): Acts as a gateway, forwarding traffic from veilnet to other devices or networks

## Monitoring and Maintenance

### Logs

The conflux uses structured logging. Check logs for detailed information:

```bash
# Docker logs
docker logs veilnet-conflux -f

# System logs (if running as service)
sudo journalctl -u veilnet-conflux -f

# Direct logs
sudo ./veilnet-conflux up 2>&1 | tee veilnet.log
```

### Graceful Shutdown

The conflux handles shutdown signals (SIGINT, SIGTERM) gracefully:

1. **Stops Anchor**: Disconnects from Guardian service
2. **Cleans Routes**: Removes all VeilNet-related network routes
3. **Removes Interface**: Deletes the TUN interface
4. **Restores Default Route**: Restores original network configuration

### Updates

To update your conflux:

```bash
# Docker
docker-compose pull
docker-compose up -d

# Native
# Download new binary and restart
```

## Troubleshooting

### Common Issues

**Permission Denied**
```bash
# Ensure running with sudo for native installation
sudo ./veilnet-conflux up

# For Docker, ensure --privileged flag is set
```

**TUN Device Creation Failed**
```bash
# Check if TUN module is loaded (Linux)
lsmod | grep tun

# Load TUN module if needed (Linux)
sudo modprobe tun

# For Docker, ensure --privileged flag is set
```

**Network Configuration Failed**
```bash
# Check if iproute2 is installed (Linux)
which ip

# Install if missing
sudo apt install iproute2  # Ubuntu/Debian
sudo yum install iproute   # CentOS/RHEL
```

**Connection to Guardian Failed**
```bash
# Check network connectivity
curl https://guardian.veilnet.org

# Verify token is correct
# Check logs for authentication errors
```

**Route Conflicts**
```bash
# Check existing routes
ip route show

# Remove conflicting routes manually if needed
sudo ip route del default dev veilnet
```

**Registration/Unregistration Issues**
```bash
# Verify your email and password are correct
# Check that the conflux name is unique
# Ensure you have proper permissions for the plane
```

### macOS (Darwin) Specific Issues

> **⚠️ Note**: macOS support is experimental and may have additional issues.

**TUN/TAP Interface Issues**
```bash
# macOS may require additional permissions
# Check System Preferences > Security & Privacy > Privacy > Full Disk Access
# Ensure Terminal or your terminal app has full disk access
```

**Network Configuration on macOS**
```bash
# macOS uses different network configuration tools
# The conflux may not work as expected on macOS
# Consider using Docker for better compatibility
```

### Windows Specific Issues

> **⚠️ Note**: Portal mode is not supported on Windows.

**TUN Device Issues**
```bash
# Windows requires the wintun.dll driver
# The conflux automatically extracts and uses the embedded driver
```

## Support

For help and support:

- **Documentation**: [www.veilnet.org/docs](https://www.veilnet.org/docs)
- **Community**: Join the VeilNet community discussions
- **Issues**: Report bugs and issues on GitHub
- **Console Support**: Contact support through the console interface

## License

This project is licensed under the CC-BY-NC-ND-4.0 License.

## Changelog

### v1.0.0
- Initial release
- Support for Linux, macOS, and Windows
- TUN interface creation and management
- Portal and client modes
- Docker support
- Conflux registration and unregistration commands
- Supabase authentication integration
- Enhanced CLI with multiple commands (register, unregister, up)
