# NetBird ACAP for Axis Cameras

[![Release](https://img.shields.io/github/v/release/Mo3he/Axis_Cam_NetBird?style=flat)](https://github.com/Mo3he/Axis_Cam_NetBird/releases)
[![Build](https://github.com/Mo3he/Axis_Cam_NetBird/actions/workflows/build.yml/badge.svg)](https://github.com/Mo3he/Axis_Cam_NetBird/actions/workflows/build.yml)
[![License](https://img.shields.io/github/license/Mo3he/Axis_Cam_NetBird?style=flat)](LICENSE)
[![Super-Linter](https://github.com/Mo3he/Axis_Cam_NetBird/actions/workflows/super-linter.yml/badge.svg)](https://github.com/Mo3he/Axis_Cam_NetBird/actions/workflows/super-linter.yml)
[![Sponsor](https://img.shields.io/badge/Sponsor%20My%20Work-EA4AAA?style=flat&logo=github&logoColor=white)](https://github.com/sponsors/Mo3he)
[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-FFDD00?style=flat&logo=buy-me-a-coffee&logoColor=black)](https://www.buymeacoffee.com/mo3he)

An independent ACAP package that runs the NetBird client directly on Axis
cameras. It uses NetBird's embedded client and userspace netstack mode, so it
does not require root privileges, `/dev/net/tun`, `CAP_NET_ADMIN`, or kernel
WireGuard support.

> **Disclaimer:** Independent, community-developed ACAP package. Not an official
> Axis product and not affiliated with, endorsed by, or supported by Axis
> Communications AB or NetBird. Use at your own risk.

> **NetBird Notice:** NetBird is distributed under its upstream licenses. This
> repository is independent of and not endorsed by NetBird. See
> [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Table of Contents

- [Overview](#overview)
- [Compatibility](#compatibility)
- [Installation](#installation)
- [Configuration](#configuration)
- [Proxy and security](#proxy-and-security)
- [Network routes and exit nodes](#network-routes-and-exit-nodes)
- [Local self-hosted test server](#local-self-hosted-test-server)
- [Build from source](#build-from-source)
- [Known limitations](#known-limitations)
- [Links](#links)
- [License](#license)

## Overview

- Joins NetBird Cloud or a self-hosted NetBird deployment with a setup key.
- Runs the WireGuard overlay entirely inside the embedded userspace client.
- Shows management, Signal, overlay IP, and proxy state in the camera UI.
- Makes the camera reachable from the NetBird network on its overlay IP, with a
  configurable list of forwarded ports and an inbound SOCKS5 proxy.
- Exposes loopback HTTP CONNECT and SOCKS5 proxies for proxy-aware camera
  services.
- Can act as a NetBird routing peer, serving Network Routes, Networks resources,
  and exit-node routes assigned to the camera in the dashboard.
- Can use routes and exit nodes from other peers for its outbound proxies.
- Persists NetBird state under the ACAP package data directory.

## Compatibility

| Package | AXIS OS | Architecture | Status |
|---|---|---|---|
| ACAP 4 | 10.12 – 13 | aarch64 | Supported; verified on AXIS OS 13 |
| ACAP 4 | 10.12 – 13 | armv7hf | Build supported; hardware verification pending |

The package is intentionally userspace-only. It does not create an OS-level
`wt0` interface, change the camera's system routes, or require root access.

## Installation

Download the EAP matching the camera architecture from the
[Releases page](https://github.com/Mo3he/Axis_Cam_NetBird/releases), then:

1. Open the camera web interface.
2. Go to **Apps -> Add app**.
3. Upload the EAP and start the app.
4. Open the app settings and enter a NetBird setup key.

Create a setup key in the NetBird dashboard under **Access Control -> Setup
Keys**. For a self-hosted server, enter its management URL, for example
`https://netbird.example.com`.

The setup key is stored as a password parameter and is never displayed in the
settings page after saving. Use a one-use or limited setup key where possible.

## Configuration

| Parameter | Default | Description |
|---|---|---|
| Management URL | `https://api.netbird.io:443` | NetBird management endpoint |
| Setup key | empty | Key used to register this camera |
| HTTP proxy port | `18080` | Loopback HTTP CONNECT proxy port |
| SOCKS5 proxy port | `11080` | Loopback SOCKS5 proxy port |
| Forwarded camera ports | `80,443,554` | Comma-separated list, up to 16 ports, reachable on the NetBird IP |
| Inbound SOCKS5 port | `1080` | SOCKS5 port on the NetBird IP for reaching any camera port |
| Routing peer (`RoutingMode`) | `off` | `off`, `internet` (serve routes, but not to the camera's own subnets), or `full` (serve routes including the camera's own subnets) |
| Accept peer routes (`AcceptRoutes`) | `no` | `yes` lets the outbound proxies use routes and exit nodes from other peers |

Settings are available in the app page and through the VAPIX parameter API:

```sh
curl --digest -u admin:password \
  'https://camera/axis-cgi/param.cgi?action=list&group=root.NetBird_VPN'
```

Devices that do not expose `param.cgi`, such as recorder/NVR- and
access-control-class devices, are served by the app's own settings endpoint. The
settings page falls back to it automatically, and it can also be used directly:

```sh
curl --anyauth -u admin:password \
  'https://camera/local/NetBird_VPN/config/settings'

curl --anyauth -u admin:password -X POST \
  -H 'X-NetBird-Action: 1' \
  --data-urlencode 'ForwardPorts=80,443,554' \
  'https://camera/local/NetBird_VPN/config/settings'
```

Changes must include the `X-NetBird-Action` header. Requests without it get
`403`, so other websites cannot change the settings through an administrator's
browser session. The setup key is write-only: it can be set through this
endpoint but is never returned by it.

The page reports the management and Signal state, assigned NetBird overlay IP,
and active proxy address. The status endpoint is available through the ACAP
reverse proxy at `/local/NetBird_VPN/api/status`.

## Proxy and security

Once connected, the camera is reachable from the NetBird network:

| Service | Address | Purpose |
|---|---|---|
| Forwarded ports | `<netbird-ip>:80 / 443 / 554` | Camera web UI, HTTPS, and RTSP over the overlay |
| Inbound SOCKS5 | `<netbird-ip>:1080` | Reach any camera port from the NetBird network |
| Outbound HTTP CONNECT | `127.0.0.1:18080` | Camera -> NetBird for HTTP/HTTPS |
| Outbound SOCKS5 | `127.0.0.1:11080` | Camera -> NetBird for SOCKS5-aware apps |

```sh
curl -k https://<netbird-ip>/       # camera web UI over the overlay
# RTSP: rtsp://<netbird-ip>:554/...
```

> **Security:** the forwarded ports and the inbound SOCKS5 proxy are reachable by
> any authorized peer in your NetBird network. Control access with NetBird access
> policies and keep the camera behind its normal authentication.

The HTTP CONNECT proxy binds to loopback only:

```text
http://127.0.0.1:18080
```

Use it for camera services that support an HTTP proxy, such as global HTTP/HTTPS
proxy settings or an HTTP-aware integration. The SOCKS5 proxy is available at
`127.0.0.1:11080` for SOCKS5-aware services. Both proxies bind to loopback only
and are not exposed to other LAN hosts.

The camera operating system is not transparently routed through the overlay in
userspace mode; applications must use a proxy or the embedded NetBird
networking API.

### Using the proxies from other camera apps

Other ACAPs on the same camera can use the outbound proxies to reach NetBird
peers, for example to send events or metadata to a collector on the NetBird
network without exposing it publicly. Use `socks5h://` so the proxy resolves
the host name, which also makes NetBird peer names work:

```sh
curl -x socks5h://127.0.0.1:11080 http://collector.netbird.cloud:8080/events
curl -x http://127.0.0.1:18080 https://collector.netbird.cloud/events
```

Most HTTP libraries accept the same URLs through their proxy settings or the
`ALL_PROXY` / `HTTPS_PROXY` environment variables. The target peer must be
allowed by your NetBird access policies. With **Accept peer routes** enabled,
networks behind other routing peers and an exit node are reachable the same
way.

## Network routes and exit nodes

NetBird routes are assigned centrally in the NetBird dashboard; the camera only
decides whether it is willing to serve or use them. Both features are off by
default. Changes take effect after the app reconnects.

### Camera as a routing peer

Set **Routing peer** to let this camera forward traffic for other peers:

| Mode | Behavior |
|---|---|
| `off` | The camera serves no routes (default) |
| `internet` | Serves assigned routes, but drops traffic to the subnets the camera is directly attached to, such as its LAN. Use for an exit node that must not expose the camera's own network |
| `full` | Serves assigned routes, including the camera's own subnets |

`internet` only blocks the subnets on the camera's interfaces when the app
starts. Other private networks the camera can reach through its gateway, and
link-local addresses (`169.254.0.0/16`), are still reachable through an exit
node. Use NetBird access policies to control what peers may reach.

Then, in the NetBird dashboard:

- **Network Routes / Networks:** add a route or resource (for example
  `192.168.1.0/24`) with this camera as the routing peer. Requires `full` when
  the route covers the camera's own subnet.
- **Exit node:** add a `0.0.0.0/0` route with this camera as the routing peer,
  or use the peer's **Set up exit node** action.
- Add an access policy that allows the client peers to reach the route or
  resource.

Routed traffic is forwarded in userspace: the camera terminates each TCP/UDP
flow from the overlay and opens a matching connection from its own network
stack, so other LAN hosts see traffic from the camera's LAN address. No root
privileges, kernel forwarding, or NAT rules are required. ICMP (ping) through
the route needs raw-socket access, which the ACAP does not have, so use TCP or
UDP to test reachability. Throughput is limited by the camera's CPU.

> **Security:** a routing peer makes every destination its routes cover
> reachable from the peers your NetBird policies allow, using the camera's
> network access. `full` adds the camera's own LAN. Scope routes and policies
> tightly, and prefer `internet` for exit-node-only use.

The Routing section of the settings page lists the routes this camera currently
serves.

### Using routes and exit nodes from other peers

Set **Accept peer routes** to `yes` so traffic sent through the camera's
outbound HTTP CONNECT and SOCKS5 proxies can reach networks behind other
routing peers. If an exit node is distributed to this camera's group, Internet
destinations requested through the proxies leave through that exit node.

Exit nodes are used only when the route has auto-apply enabled in the
dashboard; the embedded client has no way to select an exit node manually.

Both proxies resolve host names through NetBird DNS first, so peer names
(`*.netbird.cloud`) and names from nameserver groups work. With **Accept peer
routes** enabled, a name NetBird DNS cannot resolve, for example a public name
on an account without a nameserver group, is resolved with the camera's system
DNS and the connection still goes through the overlay. A connection that no
peer route covers times out after 30 seconds. The overlay is IPv4-only, so with client-side
resolution (`socks5://` rather than `socks5h://`) make sure the client picks an
IPv4 address, for example `curl -4`.
This does not change how the camera operating system routes its own traffic;
only connections made through the NetBird proxies use the overlay. The settings
page shows the exit node in use and the routes available through peers.

## Local self-hosted test server

The `server/` directory contains a LAN-only development stack for testing with
an Axis camera on the same network. It is intentionally plain HTTP and must not
be exposed to the Internet.

```sh
cd server
podman compose up -d
curl http://192.168.1.1:8080/api/instance
```

Change the LAN address in `server/config.yaml` and `server/compose.yml` for a
different development machine. Bootstrap an admin account, create a setup key,
and enter `http://<server-ip>:8080` in the camera settings.

## Build from source

Requires Docker or Podman and the Axis ACAP Native SDK image.

```sh
./build.sh
ARCHES=aarch64 ./build.sh
```

The build produces ACAP4 packages in `releases/`:

```text
NetBird_VPN_<version>_aarch64.eap
NetBird_VPN_<version>_armv7hf.eap
```

Run Go tests independently with:

```sh
go test ./...
```

## Known limitations

- ARMv7hf needs hardware verification.
- Userspace mode does not install a kernel interface or system-wide routes.
- Serving routes relies on overriding the embedded client's built-in
  `DisableServerRoutes` setting, which the upstream embedding API does not
  expose. If an upstream update changes this, the app logs
  `routing peer unavailable` and continues without serving routes.
- ICMP is not forwarded for served routes.
- SOCKS5 and HTTP proxy traffic must target a reachable NetBird peer; the local
  server's ordinary LAN address is blocked by the camera's `BlockLANAccess`
  policy.

## Links

- [NetBird](https://netbird.io/)
- [NetBird GitHub](https://github.com/netbirdio/netbird)
- [NetBird documentation](https://docs.netbird.io/)
- [Axis Communications](https://www.axis.com/)

## License

The packaging and integration code in this repository is licensed under the BSD
3-Clause License. Bundled upstream components retain their own licenses; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
