# init-ais.sh
#!/bin/bash
set -e

echo "=== Initializing AIS Environment ==="

# Setup directories
mkdir -p /etc/aisnode/{proxy,target} /ais/disk0 /var/log/ais
chmod 755 /etc/aisnode /ais /var/log/ais

# Create and mount disk image
if [[ ! -f /ais/disk0.img ]]; then
    dd if=/dev/zero of=/ais/disk0.img bs=1G count=10
    mkfs.ext4 /ais/disk0.img
fi

# Mount disk if not already mounted
if ! mountpoint -q /ais/disk0; then
    mount -o loop /ais/disk0.img /ais/disk0
fi

# Configure AIS
cat > /etc/aisnode/target/ais.json <<EOF
{
    "confdir": "/etc/aisnode/target",
    "log_dir": "/var/log/ais",
    "host_net": {
        "hostname": "localhost",
        "port": "51080"
    },
    "fspaths": {
        "/ais/disk0": {
            "use_fs_id": true,
            "enable_readonly": false
        }
    }
}
EOF

cat > /etc/aisnode/proxy/ais.json <<EOF
{
    "confdir": "/etc/aisnode/proxy",
    "log_dir": "/var/log/ais",
    "host_net": {
        "hostname": "localhost",
        "port": "51081"
    }
}
EOF

# Set permissions
chown -R root:root /ais /etc/aisnode /var/log/ais
chmod -R 755 /ais /etc/aisnode /var/log/ais