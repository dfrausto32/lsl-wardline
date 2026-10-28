#!/bin/sh
# Installs Wardline as a systemd service on a Linux machine.
#   sudo sh install-linux.sh gateway <control-url> <one-time-code>   a server that devices connect to
#   sudo sh install-linux.sh agent   <control-url> <one-time-code>   a computer that joins the network
#   sudo sh install-linux.sh control <public-url>                    the web app
# Run it from a folder that holds the `wardline` binary and the .service files
# (or set WARDLINE_BIN). Safe to run again: it replaces the binary and restarts.
set -eu

role="${1:-}"
here="$(cd "$(dirname "$0")" && pwd)"
bin="${WARDLINE_BIN:-$here/wardline}"

usage() {
  echo "Usage: sudo sh install-linux.sh gateway <control-url> <one-time-code>" >&2
  echo "       sudo sh install-linux.sh agent <control-url> <one-time-code>" >&2
  echo "       sudo sh install-linux.sh control <public-url>" >&2
  exit 2
}

[ "$(id -u)" -eq 0 ] || { echo "Please run this with sudo." >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "This computer does not use systemd. Use the container install in docs/INSTALL.md." >&2; exit 1; }
[ -x "$bin" ] || { echo "Could not find the wardline program at $bin." >&2; exit 1; }

case "$role" in
  gateway|agent) [ $# -eq 3 ] || usage ;;
  control) [ $# -eq 2 ] || usage ;;
  *) usage ;;
esac
[ -f "$here/wardline-$role.service" ] || { echo "Missing wardline-$role.service next to this script." >&2; exit 1; }

id wardline >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin wardline

install -m 0755 "$bin" /usr/local/bin/wardline
install -d -m 0750 -o root -g wardline /etc/wardline

envfile="/etc/wardline/$role.env"
umask 077
case "$role" in
  gateway) printf 'WARDLINE_CONTROL_URL=%s\nWARDLINE_ENROLL_TOKEN=%s\n' "$2" "$3" > "$envfile.new" ;;
  agent)   printf 'WARDLINE_CONTROL_URL=%s\n' "$2" > "$envfile.new" ;;
  control) printf 'WARDLINE_PUBLIC_URL=%s\n' "$2" > "$envfile.new" ;;
esac
chown root:wardline "$envfile.new"
chmod 0640 "$envfile.new"
mv "$envfile.new" "$envfile"

if [ "$role" = agent ]; then
  # Set this computer up now, so a wrong or used-up code is reported here and not buried in a log.
  # The keys are made and kept on this computer, owned by the service's own user.
  state=/var/lib/wardline-agent
  install -d -m 0700 -o wardline -g wardline "$state"
  if [ ! -f "$state/agent.json" ]; then
    command -v runuser >/dev/null 2>&1 || { echo "This computer has no runuser program, which the setup needs." >&2; exit 1; }
    runuser -u wardline -- env WARDLINE_STATE_DIR="$state" /usr/local/bin/wardline agent enroll --control-url "$2" --token "$3"
  fi
fi

install -m 0644 "$here/wardline-$role.service" "/etc/systemd/system/wardline-$role.service"
systemctl daemon-reload
systemctl enable "wardline-$role" >/dev/null
systemctl restart "wardline-$role"

echo "Wardline $role is installed and running."
echo "See how it is doing:  systemctl status wardline-$role"
echo "Read its messages:    journalctl -u wardline-$role -f"
