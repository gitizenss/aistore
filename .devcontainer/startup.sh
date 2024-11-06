# startup.sh (update)
#!/bin/bash
set -e
# Initialize AIS if not already done
/usr/local/bin/init-ais.sh

# Start systemd
exec /lib/systemd/systemd --system --unit=basic.target