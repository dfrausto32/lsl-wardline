# Servers that update themselves

A Linux server can check for new releases every half hour and install them. It installs a release **only if
`SHA256SUMS.txt` carries a signature from your release key**, so taking over the GitHub repository is not
enough to make a server run someone else's code.

## What it does

1. Every 30 minutes (a systemd timer) `wardline update run` asks GitHub for the latest release.
2. It ignores drafts, pre-releases, anything not newer than what runs now, and any release less than an hour old.
3. It downloads the install package and `SHA256SUMS.txt`, checks the signature on the checksum list with the
   public key in `/etc/wardline/release.pub`, then checks the package against the checksum list.
4. It stops the Wardline services, copies the data folders to `<folder>.backup-before-<version>` (the newest
   three are kept), swaps the program and service files, starts the services and waits for them (and the web
   app) to answer.
5. If anything is not healthy it puts the old program and service files back, starts the old version and
   remembers the bad one (`/var/lib/wardline-update/bad-<version>`) so it is not tried again. Delete that
   file to try it again.

Only Linux servers do this. Windows computers update with the new `wardline.exe` (`agent install`).

## One-time setup, on your own computer

Make the signing key. The private half stays on your computer; keep a copy somewhere safe (a password
manager), because without it nobody can publish releases that servers will accept.

```powershell
docker compose -f compose.dev.yaml run --rm -v "$env:USERPROFILE\.wardline-signing:/keys" dev go run ./tools/sign keygen /keys
```

It prints the public key (also saved as `release.pub`). `scripts/release.ps1` signs every release with the key
in `%USERPROFILE%\.wardline-signing` (or the folder in `WARDLINE_SIGNING_DIR`) and writes `SHA256SUMS.txt.sig`.
Publish that file with the others.

## Turning it on for a server

From a release that includes the updater (unpack the Linux package first):

```bash
sudo sh install-linux.sh autoupdate '<the public key>'
```

## Looking at it, pausing it

```bash
systemctl list-timers wardline-update.timer    # when it runs next
journalctl -u wardline-update                  # what it did
sudo wardline update check                     # is a newer release ready (installs nothing)
sudo systemctl disable --now wardline-update.timer   # pause
sudo systemctl enable --now wardline-update.timer    # resume
```

## Using your own copy of Wardline

Make your own key with the command above, put your public key on your servers, and set the repository to
take releases from with `WARDLINE_UPDATE_REPO=owner/name` (or `--repo`). The default is this project's.
