#!/bin/bash
set -e

# Mount the AIS disk
mount -o loop /ais/disk0.img /ais/disk0 || true

# Ensure proper permissions
chown -R root:root /ais
chmod -R 755 /ais

# Start systemd
exec /lib/systemd/systemd --system --unit=basic.target
