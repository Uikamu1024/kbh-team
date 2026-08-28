#!/bin/sh

# Run every five minutes from cron:
# */5 * * * * /path/to/batch-cron.sh >> /path/to/log 2>&1

set -eu

curl -fsS -X POST http://localhost:8080/api/batch/run
printf '\n'
