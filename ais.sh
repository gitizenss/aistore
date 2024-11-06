#!/bin/bash
# save as check_ais_requirements.sh

echo "=== Checking AIS Prerequisites ==="

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo "❌ Script must be run as root"
    exit 1
fi

# Check required directories
echo -n "Checking directories... "
for dir in "/etc/aisnode/proxy" "/etc/aisnode/target" "/ais/disk0"; do
    if [[ ! -d "$dir" ]]; then
        echo "❌ Missing directory: $dir"
        exit 1
    fi
done
echo "✅"

# Check config files
echo -n "Checking config files... "
for file in "/etc/aisnode/proxy/ais.json" "/etc/aisnode/target/ais.json"; do
    if [[ ! -f "$file" ]]; then
        echo "❌ Missing config: $file"
        exit 1
    fi
done
echo "✅"

# Check disk image
echo -n "Checking disk image... "
if [[ ! -f "/ais/disk0.img" ]]; then
    echo "❌ Missing disk image"
    exit 1
fi
echo "✅"

# Check mount status
echo -n "Checking mount... "
if ! mountpoint -q /ais/disk0; then
    echo "❌ /ais/disk0 is not mounted"
    exit 1
fi
echo "✅"

# Check filesystem type
echo -n "Checking filesystem... "
fs_type=$(df -T /ais/disk0 | tail -n 1 | awk '{print $2}')
if [[ "$fs_type" == "overlay" ]]; then
    echo "❌ Filesystem is overlay instead of ext4"
    exit 1
fi
echo "✅ ($fs_type)"

# Check available space
echo -n "Checking disk space... "
available_space=$(df -BG /ais/disk0 | tail -n 1 | awk '{print $4}' | sed 's/G//')
if (( available_space < 5 )); then
    echo "❌ Less than 5GB available"
    exit 1
fi
echo "✅ (${available_space}GB available)"

echo "=== All checks completed successfully ==="