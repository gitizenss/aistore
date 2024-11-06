#!/bin/bash
set -e
# Ensure the log directory exists and has the correct permissions
LOG_DIR="/var/log/myapp"
mkdir -p $LOG_DIR
chmod 777 $LOG_DIR

# Run the windmill program with the necessary environment variables
export WORKER_GROUP=reports
export MODE=worker
export DATABASE_URL="postgres://postgres:changeme@89.117.77.162:5432/windmill?sslmode=disable"

windmill &