#!/bin/bash
# =============================================================================
# Development Machine IP Detection Script
# =============================================================================
# Description: Automatically detect the development machine's LAN IP address
# Usage: ./detect-dev-ip.sh [--target-server SERVER_IP]
# =============================================================================

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

TARGET_SERVER=""
VERBOSE=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --target-server)
            TARGET_SERVER="$2"
            shift 2
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}" >&2
            exit 1
            ;;
    esac
done

# Function to log verbose messages
log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${CYAN}[DEBUG] $1${NC}" >&2
    fi
}

# Function to check if IP is in private range
is_private_ip() {
    local ip=$1
    if [[ $ip =~ ^192\.168\. ]] || \
       [[ $ip =~ ^10\. ]] || \
       [[ $ip =~ ^172\.(1[6-9]|2[0-9]|3[0-1])\. ]]; then
        return 0
    fi
    return 1
}

# Function to check if IP is reachable from target server
is_reachable_from_target() {
    local ip=$1
    local target=$2

    if [ -z "$target" ]; then
        # No target specified, assume reachable
        return 0
    fi

    # Extract network prefix (e.g., 192.168.1.x -> 192.168.1)
    local ip_prefix=$(echo "$ip" | cut -d'.' -f1-3)
    local target_prefix=$(echo "$target" | cut -d'.' -f1-3)

    if [ "$ip_prefix" = "$target_prefix" ]; then
        # Same subnet
        return 0
    fi

    # Different subnet - may still be routable
    # In most cases, if both are private IPs, they might not be routable
    return 1
}

# Detect OS
if [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
    log_verbose "Detected OS: macOS"
elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
    log_verbose "Detected OS: Linux"
else
    echo -e "${RED}Error: Unsupported OS: $OSTYPE${NC}" >&2
    exit 1
fi

# Get all network interface IPs
if [ "$OS" = "macos" ]; then
    ALL_IPS=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}')
else
    ALL_IPS=$(ip addr | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | cut -d'/' -f1)
fi

log_verbose "All detected IPs: $ALL_IPS"

# Filter to private IPs only
PRIVATE_IPS=()
for ip in $ALL_IPS; do
    if is_private_ip "$ip"; then
        PRIVATE_IPS+=("$ip")
        log_verbose "Found private IP: $ip"
    fi
done

if [ ${#PRIVATE_IPS[@]} -eq 0 ]; then
    echo -e "${RED}Error: No private IP addresses found${NC}" >&2
    echo -e "${YELLOW}Please ensure you're connected to a network${NC}" >&2
    exit 1
fi

# If target server specified, filter to same subnet
if [ -n "$TARGET_SERVER" ]; then
    log_verbose "Target server specified: $TARGET_SERVER"

    SAME_SUBNET_IPS=()
    for ip in "${PRIVATE_IPS[@]}"; do
        if is_reachable_from_target "$ip" "$TARGET_SERVER"; then
            SAME_SUBNET_IPS+=("$ip")
            log_verbose "IP $ip is in same subnet as target"
        fi
    done

    if [ ${#SAME_SUBNET_IPS[@]} -gt 0 ]; then
        # Use first IP from same subnet
        SELECTED_IP="${SAME_SUBNET_IPS[0]}"
        log_verbose "Selected IP from same subnet: $SELECTED_IP"
    else
        # No same-subnet IP, use first private IP and warn
        SELECTED_IP="${PRIVATE_IPS[0]}"
        log_verbose "WARNING: No same-subnet IP found, using: $SELECTED_IP"
        echo -e "${YELLOW}Warning: Dev machine ($SELECTED_IP) and target ($TARGET_SERVER) are in different subnets${NC}" >&2
        echo -e "${YELLOW}Registry may not be accessible from target server${NC}" >&2
    fi
else
    # No target specified, prefer 192.168.x.x, then 10.x.x.x, then 172.x.x.x
    log_verbose "No target server specified, using priority order"

    for ip in "${PRIVATE_IPS[@]}"; do
        if [[ $ip =~ ^192\.168\. ]]; then
            SELECTED_IP="$ip"
            log_verbose "Selected 192.168.x.x IP: $SELECTED_IP"
            break
        fi
    done

    if [ -z "$SELECTED_IP" ]; then
        for ip in "${PRIVATE_IPS[@]}"; do
            if [[ $ip =~ ^10\. ]]; then
                SELECTED_IP="$ip"
                log_verbose "Selected 10.x.x.x IP: $SELECTED_IP"
                break
            fi
        done
    fi

    if [ -z "$SELECTED_IP" ]; then
        SELECTED_IP="${PRIVATE_IPS[0]}"
        log_verbose "Selected first available IP: $SELECTED_IP"
    fi
fi

# Output the selected IP
echo "$SELECTED_IP"

if [ "$VERBOSE" = true ]; then
    echo -e "${GREEN}✓ Detected development machine IP: $SELECTED_IP${NC}" >&2
fi

exit 0
