# Zion Node

Zion Node is the execution layer of the Zion Protocol, responsible for running AI agents in containerized environments and reporting state to the Hub.

## Install

### One-liner (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/THEZIONLABS/Zion-Node/main/scripts/install.sh | bash
```

### Build from source

```bash
./scripts/build.sh
```

This compiles the node binary to `./release/zion-node`.

## Quick Start

### 1. Create Wallet

```bash
zion-node wallet new
```

This generates a new Ethereum wallet and saves it to `~/.zion-node/wallet.json`.

**⚠️ IMPORTANT:** Save your private key securely! It's displayed only once.

### 2. Configure

```bash
cp config.example.toml config.toml
```

Edit `config.toml` and update:
- `hub_url`: Set to your Hub endpoint (e.g., `https://hub.zion.example`)
- `node_id`: Choose a unique identifier for your node

### 3. Run

```bash
zion-node --config config.toml
```

The node will:
1. Automatically authenticate with the Hub using your wallet
2. Register itself with the Hub
3. Start accepting agent assignments
4. Send periodic heartbeats

### 5. Create an Agent (Optional)

To quickly create and run a test agent:

```bash
./scripts/create-agent.sh --config config.toml
```

This will authenticate with your wallet, create an agent, and automatically run it on your node.

## Configuration

See [docs/CONFIG.md](docs/CONFIG.md) for detailed configuration options.

Key settings:
- **Hub Connection**: `hub_url` (signing key is auto-fetched at startup)
- **Capacity**: `max_agents`, `cpu_per_agent`, `memory_per_agent`
- **Storage**: `data_dir`, `snapshot_cache`
- **Container**: `container_engine`, `runtime_image`

## Wallet Management

```bash
# Create new wallet
zion-node wallet new

# Show existing wallet address
zion-node wallet show

# Export private key
zion-node wallet export
```

See [docs/WALLET_README.md](docs/WALLET_README.md) for more details.

## Local Development

For local development with a local Hub instance:

```bash
# Use local config
cp config.local.toml config.toml

# Start the Hub separately (see Hub repo documentation)

# Run the node
zion-node --config config.toml
```

See [docs/QUICKSTART.md](docs/QUICKSTART.md) for detailed local development setup.

## Requirements

- **Go 1.21+**
- **Docker** (for running agent containers)
- **Linux/macOS** (Windows via WSL2)

## Architecture

```
┌─────────────────┐
│   Zion Hub      │  ← Orchestration layer
└────────┬────────┘
         │
    ┌────┴────┬────────┬─────────┐
    │         │        │         │
┌───▼───┐ ┌──▼───┐ ┌──▼───┐ ┌───▼───┐
│ Node  │ │ Node │ │ Node │ │ Node  │  ← Execution layer
└───┬───┘ └──┬───┘ └──┬───┘ └───┬───┘
    │        │        │         │
┌───▼───┐ ┌──▼───┐ ┌──▼───┐ ┌───▼───┐
│Agent 1│ │Agent2│ │Agent3│ │Agent 4│  ← AI agent containers
└───────┘ └──────┘ └──────┘ └───────┘
```

Each node:
- Manages multiple agent containers
- Reports agent state to Hub via heartbeats
- Executes commands from Hub (start/stop/snapshot agents)
- Verifies Hub command signatures for security

## Security

### Command Signing

All commands from Hub to Node are cryptographically signed using ECDSA secp256k1:

- Hub signs commands with its private signing key
- Node automatically fetches the Hub's public key at startup via `GET /v1/system/signing-key`
- The public key is stored in memory only — no manual configuration required
- Prevents command injection and man-in-the-middle attacks

### Wallet Authentication

- Node authenticates with Hub using EIP-191 message signing
- Private key never leaves the node
- JWT tokens for API access

> **⚠️ Security Warning:** The wallet private key is stored in plaintext at `~/.zion-node/wallet.json` (file permissions 0600). This is suitable for development and testnet use. For production environments with significant value at stake, consider using a hardware wallet or external key management service. **Never commit wallet files to version control.**

### Anti-Cheat Participation

Node participates in Hub's anti-cheat verification:

- **Binary Attestation** — On registration, Node computes SHA-256 of its own binary and reports `binary_hash` to Hub for integrity verification
- **Agent Probe Response** — When Hub sends a `probe` command via heartbeat, Node verifies the target agent container is actually running (via `agentManager.GetAgent()`), then responds with the challenge nonce through a `probe_response` event
- **Capacity Reporting** — Node reports `system_cpu` and `system_memory_mb` at registration; Hub uses these to cap `total_slots` server-side

See Hub's [anti-cheat documentation](../hub/docs/anti-cheat.md) for the complete security architecture.

## Documentation

- **[QUICKSTART.md](docs/QUICKSTART.md)** — Local development setup
- **[CONFIG.md](docs/CONFIG.md)** — Configuration reference
- **[WALLET_README.md](docs/WALLET_README.md)** — Wallet management
- **[E2E_TEST_README.md](docs/E2E_TEST_README.md)** — Testing guide

## Monitoring

The node exposes a REST API on port 9000 (configurable):

```bash
# Check node status
curl http://localhost:9000/health

# List agents
curl http://localhost:9000/agents

# Agent details
curl http://localhost:9000/agents/{agent_id}
```

## Troubleshooting

### Docker not running
```
FATA[0000] Docker daemon is not running
```
**Solution:** Start Docker service: `sudo systemctl start docker`

### Hub connection failed
```
WARN[0001] Failed to connect to hub: connection refused
```
**Solution:** Verify `hub_url` is correct and Hub is running

## License

MIT
