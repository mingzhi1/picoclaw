#!/bin/sh
set -e

METACLAW_HOME="${METACLAW_HOME:-${HOME}/.metaclaw}"

# First-run: neither config nor workspace exists.
# If config.json is already mounted but workspace is missing we skip onboard
# to avoid the interactive prompt hanging in a non-TTY container.
if [ ! -d "${METACLAW_HOME}/workspace" ] && [ ! -f "${METACLAW_HOME}/config.json" ]; then
    metaclaw onboard
    echo ""
    echo "First-run setup complete."
    echo "Edit ${METACLAW_HOME}/config.json (add your API key, etc.) then restart the container."
    exit 0
fi

exec metaclaw "$@"
