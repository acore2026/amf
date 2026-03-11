# free5gc-compose + UERANSIM Setup Guide

This guide covers setting up free5GC core network with Docker Compose and UERANSIM for testing AMF modifications.

**Official Resources:**
- [free5GC Compose Guide](https://free5gc.org/guide/0-compose/)
- [UERANSIM Installation](https://free5gc.org/guide/5-install-ueransim/)
- [GTP5G Kernel Module](https://github.com/free5gc/gtp5g)

---

## Prerequisites

### System Requirements

```bash
# Check kernel version (must be 5.0.23+ or 5.4.x+)
uname -r

# Check for AVX support (required for MongoDB)
grep avx /proc/cpuinfo

# Check Docker is installed
docker --version
docker compose version
```

**Supported kernels**: `5.0.0-23-generic` or `5.4.x`

### Install Dependencies

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install -y git gcc g++ make cmake autoconf libtool pkg-config \
    libmnl-dev libyaml-dev libsctp-dev lksctp-tools iproute2

# Fedora/RHEL
sudo dnf install -y git gcc gcc-c++ make cmake autoconf libtool pkg-config \
    libmnl-devel libyaml-devel lksctp-tools iproute
```

---

## Step 1: Install gtp5g Kernel Module

The **gtp5g** kernel module is essential for UPF data plane functionality.

```bash
# Clone gtp5g repository
cd ~
git clone -b v0.9.14 https://github.com/free5gc/gtp5g.git
cd gtp5g

# Build and install
make
sudo make install

# Load the module
sudo modprobe gtp5g

# Verify installation
lsmod | grep gtp5g
# Output: gtp5g                 114688  0
```

**Note**: You may need to reinstall after kernel updates.

---

## Step 2: Clone and Configure free5gc-compose

### 2.1 Clone Repository

```bash
cd ~
git clone https://github.com/free5gc/free5gc-compose.git
cd free5gc-compose

# Optional: Use stable release (recommended)
git checkout v3.4.2  # or check latest release
```

### 2.2 Configure Host Networking

```bash
# Enable IP forwarding
sudo sysctl -w net.ipv4.ip_forward=1

# Get your network interface name
ip route | grep default
# Example output: default via 192.168.1.1 dev eth0

# Run the provided script (replace <interface> with yours, e.g., eth0)
sudo ./reload_host_config.sh <interface>

# Or manually configure iptables
sudo iptables -t nat -A POSTROUTING -o <interface> -j MASQUERADE
sudo iptables -A FORWARD -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1400
```

---

## Step 3: Start free5GC Core

### Option A: Using Pre-built Images (Quickest)

```bash
cd ~/free5gc-compose

# Start all services
sudo docker compose up -d

# Or run in foreground to see logs
sudo docker compose up
```

### Option B: Build from Source (For AMF Modifications)

Since you want to test AMF modifications, build with your local changes:

```bash
cd ~/free5gc-compose

# Clone free5gc source (includes AMF)
cd base
git clone --recursive -j $(nproc) https://github.com/free5gc/free5gc.git
cd ..

# Replace AMF source with your modified version
rm -rf base/free5gc/NFs/amf
cp -r /path/to/your/amf base/free5gc/NFs/amf

# Build images
make base
docker compose -f docker-compose-build.yaml build

# Run with locally built images
sudo docker compose -f docker-compose-build.yaml up -d
```

### Verify Core is Running

```bash
# Check all containers
docker ps

# Check AMF logs specifically
docker logs amf -f

# Expected output shows:
# - AMF initialization
# - NGAP service started on :38412
# - Registered with NRF
```

---

## Step 4: Install UERANSIM (UE/gNB Simulator)

### 4.1 Clone and Build

```bash
cd ~
git clone https://github.com/aligungr/UERANSIM.git
cd UERANSIM

# Checkout compatible version for free5GC v3.4.0+
git checkout e4c492d

# Install dependencies and build
sudo apt update
sudo apt install -y make g++ libsctp-dev lksctp-tools iproute2
sudo snap install cmake --classic

make
```

### 4.2 Get Network Configuration

```bash
# Get AMF IP address from container
docker inspect amf | grep -A 5 '"Networks"' | grep "IPAddress"
# Default: 10.100.200.16

# Get your host IP (for gNB binding)
ip addr show | grep "inet " | grep -v "127.0.0.1"
# Example: 192.168.56.102
```

### 4.3 Configure gNB

Create `~/UERANSIM/config/free5gc-gnb.yaml`:

```yaml
mcc: '208'          # Mobile Country Code
mnc: '93'           # Mobile Network Code
nci: '0x000000010'  # NR Cell Identity
idLength: 32
tac: 1              # Tracking Area Code

# gNB IP addresses (your host machine IP)
ngapIp: 192.168.56.102    # CHANGE TO YOUR HOST IP
gtpIp: 192.168.56.102     # CHANGE TO YOUR HOST IP

# AMF Configuration
amfConfigs:
  - address: 10.100.200.16   # AMF container IP
    port: 38412

# Supported S-NSSAI slices
slices:
  - sst: 0x01
    sd: 0x010203

# Link configuration
links:
  - amf:
      ngapIp: 10.100.200.16
      ngapPort: 38412
```

### 4.4 Configure UE

Create `~/UERANSIM/config/free5gc-ue.yaml`:

```yaml
# UE Identification
supi: 'imsi-208930000000003'
mcc: '208'
mnc: '93'
key: '8baf473f2f8fd09487cccbd7097c6862'
op: '8e27b6af0e692e750f32667a3b14605d'
opType: 'OP'
amf: '8000'
imei: '356938035643803'
imeisv: '4370816125816151'

# gNB IP address
gnbSearchList:
  - 192.168.56.102    # CHANGE TO YOUR HOST IP

# UAC Configuration
uacAic:
  mps: false
  mcs: false
uacAcc:
  normalClass: 0
  class11: false
  class12: false
  class13: false
  class14: false
  class15: false

# PDU Session Configuration
sessions:
  - type: 'IPv4'
    apn: 'internet'
    slice:
      sst: 0x01
      sd: 0x010203

# NSSAI Configuration
configured-nssai:
  - sst: 0x01
    sd: 0x010203

default-nssai:
  - sst: 1
    sd: 1

# Security Algorithms
integrity:
  IA1: true
  IA2: true
  IA3: true
ciphering:
  EA1: true
  EA2: true
  EA3: true

# Integrity protection maximum data rate
integrityMaxRate:
  uplink: 'full'
  downlink: 'full'
```

---

## Step 5: Add Subscriber via WebUI

### 5.1 Access WebUI

```bash
# If running locally
http://localhost:5000

# If on remote VM, port forward
ssh -L 5000:localhost:5000 user@vm-address
```

**Login credentials:**
- Username: `admin`
- Password: `free5gc`

### 5.2 Create Subscriber

1. Click **Subscribers** -> **New Subscriber**
2. **Critical Settings:**
   - Change `Operator Code Type` from **OPc** to **OP**
   - **IMSI**: `208930000000003`
   - **OP**: `8e27b6af0e692e750f32667a3b14605d`
   - **Key**: `8baf473f2f8fd09487cccbd7097c6862`
   - **AMF**: `8000`
   - **SST**: `1`
   - **SD**: `010203`
3. Click **Submit**

---

## Step 6: Run Registration Test

### Terminal 1: Start gNB

```bash
cd ~/UERANSIM/build
./nr-gnb -c ../config/free5gc-gnb.yaml
```

**Expected output:**
```
[info] Trying to establish SCTP connection... (10.100.200.16:38412)
[info] SCTP connection established (10.100.200.16:38412)
[info] NG Setup procedure is successful
```

### Terminal 2: Start UE

```bash
cd ~/UERANSIM/build
sudo ./nr-ue -c ../config/free5gc-ue.yaml
```

**Expected successful registration:**
```
[nas] [info] UE switches to state: MM-DEREGISTERED/PLMN-SEARCH
[nas] [info] UE switches to state: MM-DEREGISTERED/NORMAL-SERVICE
[rrc] [info] RRC connection established
[nas] [info] UE switches to state: MM-REGISTERED-INITIATED/NA
[nas] [info] UE switches to state: MM-REGISTERED/NORMAL-SERVICE
[nas] [info] Initial Registration is successful
[nas] [info] PDU Session establishment is successful PSI[1]
[app] [info] TUN interface[uesimtun0, 10.60.0.1] is up
```

### Terminal 3: Verify Connectivity

```bash
# Check TUN interface
ifconfig uesimtun0

# Test connectivity through 5G
ping -I uesimtun0 8.8.8.8

# Or browse via 5G
curl --interface uesimtun0 https://www.google.com
```

---

## Step 7: Verify AMF Logs

To confirm your AMF is handling the registration:

```bash
# Watch AMF logs
docker logs amf -f

# Look for:
# - "UE Context created"
# - "Handle Initial Registration"
# - "Registration complete"
# - State transitions in GMM
```

---

## Quick Reference: Development Workflow

Once this setup works, iterate on AMF development:

```bash
# 1. Modify AMF code in /home/acore/proj/go/amf

# 2. Rebuild the AMF image
cd ~/free5gc-compose
rm -rf base/free5gc/NFs/amf
cp -r /home/acore/proj/go/amf base/free5gc/NFs/amf
docker compose -f docker-compose-build.yaml build amf

# 3. Restart just the AMF
docker compose -f docker-compose-build.yaml up -d amf

# 4. Test again with UERANSIM
# (gNB and UE configs should remain the same)
```

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| **GTP5G module not found** | Reinstall: `cd ~/gtp5g && sudo make install && sudo modprobe gtp5g` |
| **SCTP connection refused** | Check AMF is running: `docker ps`. Verify AMF IP with `docker inspect amf` |
| **Registration rejected** | Verify subscriber data matches exactly (OP type, keys, IMSI) |
| **PDU session fails** | Check UPF logs: `docker logs upf`. Ensure gtp5g is loaded |
| **No internet through 5G** | Check iptables rules and IP forwarding |
| **MongoDB AVX error** | Use older MongoDB version or different hardware |
| **AMF IP changes** | AMF container gets new IP on restart - update gNB config |

### Reset Everything

```bash
# Stop all
cd ~/free5gc-compose
docker compose down

# Clear database
docker volume rm free5gc-compose_dbdata

# Restart
sudo docker compose up -d
```

---

## Configuration Summary

| Parameter | Location | Example Value | Notes |
|-----------|----------|---------------|-------|
| **AMF IP** | `free5gc-gnb.yaml` | `10.100.200.16` | From `docker inspect amf` |
| **gNB NGAP/GTP IP** | `free5gc-gnb.yaml` | `192.168.56.102` | Host machine IP |
| **IMSI** | WebUI & `free5gc-ue.yaml` | `208930000000003` | Must match exactly |
| **OP Type** | WebUI | Change to `OP` | Default is OPc |
| **S-NSSAI** | All configs | `SST: 1, SD: 010203` | Must match across configs |

---

**Created**: 2026-03-11
**For**: free5gc-amf development and testing
