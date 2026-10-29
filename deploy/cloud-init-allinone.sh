#!/bin/bash
# Start-up script for a small cloud server (used as the "user data" when the server is created; see docs/AWS-LIGHTSAIL.md).
# Set WARDLINE_VERSION and NAME below for your own release and name.
# Runs once, as root, the first time the server starts. Its output is kept in /var/log/wardline-setup.log.
set -eux
exec > /var/log/wardline-setup.log 2>&1

VERSION=${WARDLINE_VERSION:-v0.1.7}
NAME=network.laneysoftwarelogistics.com
BASE=https://github.com/dfrausto32/lsl-wardline/releases/download/$VERSION

# The smallest server has little memory, so give it a swap file.
fallocate -l 1G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y nftables caddy curl ca-certificates

# Download Wardline and check it against the published checksum before running anything.
mkdir -p /opt/wardline-install
cd /opt/wardline-install
curl -fsSL -O "$BASE/wardline-linux-install.tar.gz"
curl -fsSL -O "$BASE/SHA256SUMS.txt"
tr -d '' < SHA256SUMS.txt | grep ' wardline-linux-install.tar.gz$' | sha256sum -c -
tar -xzf wardline-linux-install.tar.gz

# The web app and the server, as one service. The web app only listens inside the machine; Caddy fronts it.
sh install-linux.sh allinone "https://$NAME"
echo 'WARDLINE_LISTEN=127.0.0.1:8080' >> /etc/wardline/allinone.env
systemctl restart wardline-allinone

# HTTPS. Caddy gets and renews the certificate by itself once the name points at this server.
cat > /etc/caddy/Caddyfile <<EOF
$NAME {
	reverse_proxy 127.0.0.1:8080
}
EOF
systemctl restart caddy

echo "wardline setup finished"
