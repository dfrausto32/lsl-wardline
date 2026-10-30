#!/bin/sh
# Installs Wardline as a systemd service on a Linux machine.
#   sudo sh install-linux.sh gateway <control-url> <one-time-code>   a server that devices connect to
#   sudo sh install-linux.sh agent   <control-url> <one-time-code>   a computer that joins the network
#   sudo sh install-linux.sh control <public-url>                    the web app
#   sudo sh install-linux.sh allinone <public-url>                   the web app and the server on one computer (a cloud server)
#   sudo sh install-linux.sh autoupdate <release-public-key>         keep this computer up to date from releases signed with that key
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
  echo "       sudo sh install-linux.sh allinone <public-url>" >&2
  exit 2
}

[ "$(id -u)" -eq 0 ] || { echo "Please run this with sudo." >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "This computer does not use systemd. Use the container install in docs/INSTALL.md." >&2; exit 1; }
[ -x "$bin" ] || { echo "Could not find the wardline program at $bin." >&2; exit 1; }

if [ "$role" = autoupdate ]; then
  # Turns on automatic updates from signed releases. The argument is the release public key (text, from "wardline-sign keygen").
  [ $# -eq 2 ] || usage
  [ -f "$here/wardline-update.service" ] && [ -f "$here/wardline-update.timer" ] || { echo "Missing the wardline-update files next to this script." >&2; exit 1; }
  install -m 0755 "$bin" /usr/local/bin/wardline
  install -d -m 0755 /etc/wardline
  printf '%s\n' "$2" > /etc/wardline/release.pub
  chmod 0644 /etc/wardline/release.pub
  install -m 0644 "$here/wardline-update.service" /etc/systemd/system/wardline-update.service
  install -m 0644 "$here/wardline-update.timer" /etc/systemd/system/wardline-update.timer
  systemctl daemon-reload
  systemctl enable --now wardline-update.timer >/dev/null
  echo "Automatic updates are on. This computer checks every half hour and installs a release only if it is signed with your key."
  echo "Look at it:   systemctl list-timers wardline-update.timer      journalctl -u wardline-update"
  echo "Pause it:     sudo systemctl disable --now wardline-update.timer"
  exit 0
fi

case "$role" in
  gateway|agent) [ $# -eq 3 ] || usage ;;
  control|allinone) [ $# -eq 2 ] || usage ;;
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
  control|allinone) printf 'WARDLINE_PUBLIC_URL=%s\n' "$2" > "$envfile.new" ;;
esac
chown root:wardline "$envfile.new"
chmod 0640 "$envfile.new"
mv "$envfile.new" "$envfile"

if [ "$role" = agent ]; then
  # Let the service set this computer's name settings for its own interface, so machines can be reached by name.
  # Both polkit formats are installed; the one this computer's polkit does not read is ignored.
  if [ -d /etc/polkit-1/rules.d ] && [ -f "$here/wardline-names.rules" ]; then
    install -m 0644 "$here/wardline-names.rules" /etc/polkit-1/rules.d/50-wardline-names.rules
  fi
  if [ -d /etc/polkit-1/localauthority ] && [ -f "$here/wardline-names.pkla" ]; then
    install -d -m 0755 /etc/polkit-1/localauthority/50-local.d
    install -m 0644 "$here/wardline-names.pkla" /etc/polkit-1/localauthority/50-local.d/50-wardline-names.pkla
  fi
fi

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
