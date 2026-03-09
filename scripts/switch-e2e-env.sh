#!/bin/bash
# Switch Zion Node config between local and remote Hub
# Usage: ./scripts/switch-e2e-env.sh [local|remote]
#
# This script copies config.local.toml or config.remote.toml to config.toml.
# Alternatively, you can use: ./zion-node --config config.local.toml

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_DIR"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ENV_TYPE="${1:-}"

# Show current environment
if [ -f "config.toml" ]; then
    CURRENT_ENDPOINT=$(grep '^hub_url' config.toml 2>/dev/null | sed 's/.*= *"\(.*\)"/\1/' || echo "")
    if [ -n "$CURRENT_ENDPOINT" ]; then
        if [[ "$CURRENT_ENDPOINT" == *"localhost"* ]] || [[ "$CURRENT_ENDPOINT" == *"127.0.0.1"* ]]; then
            CURRENT_TYPE="local"
        else
            CURRENT_TYPE="remote"
        fi
        echo -e "${BLUE}Current environment: ${CURRENT_TYPE} (${CURRENT_ENDPOINT})${NC}"
    else
        echo -e "${YELLOW}Current environment: unknown${NC}"
    fi
else
    echo -e "${YELLOW}No config.toml found${NC}"
fi

# If no argument provided, show menu
if [ -z "$ENV_TYPE" ]; then
    echo ""
    echo "Available environments:"
    echo "  1) local  - Use local Hub (http://localhost:3000)"
    echo "  2) remote - Use remote Hub (from config.remote.toml)"
    echo ""
    read -p "Select environment [1-2]: " choice

    case $choice in
        1) ENV_TYPE="local" ;;
        2) ENV_TYPE="remote" ;;
        *)
            echo -e "${RED}Invalid choice${NC}"
            exit 1
            ;;
    esac
fi

# Validate environment type
if [ "$ENV_TYPE" != "local" ] && [ "$ENV_TYPE" != "remote" ]; then
    echo -e "${RED}Error: Environment must be 'local' or 'remote'${NC}"
    echo ""
    echo "Usage: $0 [local|remote]"
    echo ""
    echo "Or use --config flag directly:"
    echo "  ./zion-node --config config.local.toml"
    echo "  ./zion-node --config config.remote.toml"
    exit 1
fi

# Check if source file exists
SOURCE_FILE="config.${ENV_TYPE}.toml"
if [ ! -f "$SOURCE_FILE" ]; then
    echo -e "${RED}Error: ${SOURCE_FILE} not found${NC}"
    echo "Create it from config.example.toml:"
    echo "  cp config.example.toml ${SOURCE_FILE}"
    exit 1
fi

# Copy config file
cp "$SOURCE_FILE" config.toml
echo -e "${GREEN}✓ Switched to ${ENV_TYPE} environment${NC}"

# Show configuration
echo ""
echo -e "${BLUE}Current configuration:${NC}"
grep -E '^hub_url' config.toml | head -1 || echo "  hub_url: (not set)"
grep -E '^node_id' config.toml | head -1 || echo "  node_id: (not set)"
grep -E '^log_level' config.toml | head -1 || echo "  log_level: (not set)"

echo ""
echo -e "${GREEN}Config switched successfully!${NC}"
echo ""
echo "Next steps:"
if [ "$ENV_TYPE" == "local" ]; then
    echo "  1. Make sure local Hub is running: cd hub && npm run dev"
    echo "  2. Start node: ./zion-node"
    echo "  3. Or run tests: ./scripts/run-e2e-tests.sh full"
else
    echo "  1. Ensure you have network access to the remote Hub"
    echo "  2. Start node: ./zion-node"
    echo "  3. Or run tests: ./scripts/run-e2e-tests.sh full"
fi
echo ""
echo "Tip: You can also use --config flag without switching:"
echo "  ./zion-node --config config.${ENV_TYPE}.toml"
