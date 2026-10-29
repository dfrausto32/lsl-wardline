# Running Wardline on a small cloud server (AWS Lightsail)

Use this when your own internet connection cannot be reached from outside (for example Starlink's shared address). Every computer, including your own, connects out to one small server with a normal public address, so nothing at home needs forwarding.

The web app and the server run together as one service (`allinone`), and the server registers itself, so there is no command to copy between them.

Prices and bundle names change. Every step that costs money or opens a port is listed here so it can be approved first.

## What you need first
1. **Your own AWS account**, separate from any employer's or client's. Turn on multi-factor sign-in for the root user, and set a budget alert (for example $10 a month) under Billing.
2. **Command-line access for that account.** Create a limited sign-in that only has Lightsail permissions (the `AmazonLightsailFullAccess` policy), then run `aws configure sso --profile wardline-personal` (or `aws configure --profile wardline-personal` for access keys). Never paste keys into chat. Tell the assistant only the profile name.
3. **A name** such as `network.laneysoftwarelogistics.com`. A static address means you only add it once, as an A record in your registrar's DNS settings.

## The steps (each one is shown and approved before it is run)
Region: `us-east-2` (Ohio).

1. **Read only: confirm what is available and the price.**
   `aws lightsail get-blueprints --region us-east-2 --profile wardline-personal` (look for Ubuntu 24.04)
   `aws lightsail get-bundles --region us-east-2 --profile wardline-personal` (find the smallest bundle with an IPv4 address; as of writing about $5 a month, but check the price it shows)
2. **Costs money: create the server.** One instance, Ubuntu, the smallest IPv4 bundle, in `us-east-2a`.
3. **Costs nothing while attached: reserve a static address and attach it** to the server. This keeps the address from changing when the server restarts.
4. **Opens ports: set the firewall.** Allow UDP 51820 (devices), TCP 80 and 443 (the web app, through the HTTPS front), and TCP 22 only from your own address. TCP 8080 stays closed to the internet.
5. **Your DNS:** add an A record `network` pointing at the static address.
6. **On the server (over SSH):**
   - Download the release bundle and run `sudo sh install-linux.sh allinone https://network.laneysoftwarelogistics.com`.
   - Add `WARDLINE_LISTEN=127.0.0.1:8080` to `/etc/wardline/allinone.env` and restart `wardline-allinone`, so the web app is only reachable through the HTTPS front.
   - Install Caddy and give it a two-line config that serves your name and forwards to `127.0.0.1:8080`. Caddy gets and renews the HTTPS certificate by itself.
   - Install `nftables` (`sudo apt install nftables`) if it is not already there; relaying between devices needs it.
7. **In your browser:** open `https://network.laneysoftwarelogistics.com`, create your sign-in, name the network, and use that same name as the server address. The server registers itself.
8. **Your own computers:** on Windows, double-click `wardline.exe` and choose **Add this computer to my network** with the command from **Add a device**. Turn on **Let devices reach each other** on the server page so your machines can reach one another.

## Taking it away again
`aws lightsail delete-instance`, then release the static address. Stopping the server alone still charges for it.

## Not verified yet
None of this has run on a real server yet. The `allinone` service and install role are tested only as far as the scripts and unit files go, and the HTTPS front, the firewall rules and the first connection are the things to check when we do it for real.
