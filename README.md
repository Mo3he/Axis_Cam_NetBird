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

> **Disclaimer:** This is not an official Axis Communications or NetBird
> product. Use it at your own risk.

> **NetBird notice:** NetBird is distributed under its upstream licenses. This
> repository is independent of and not endorsed by NetBird. See
> [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Table of Contents

- [Overview](#overview)
- [Compatibility](#compatibility)
- [Installation](#installation)
- [Configuration](#configuration)
- [Proxy and security](#proxy-and-security)
- [Local self-hosted test server](#local-self-hosted-test-server)
- [Build from source](#build-from-source)
- [Known limitations](#known-limitations)
- [Links](#links)
- [License](#license)

## Overview

- Joins NetBird Cloud or a self-hosted NetBird deployment with a setup key.
- Runs the WireGuard overlay entirely inside the embedded userspace client.
- Shows management, Signal, overlay IP, and proxy state in the camera UI.
- Exposes loopback HTTP CONNECT and SOCKS5 proxies for proxy-aware camera
  services.
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

Settings are available in the app page and through the VAPIX parameter API:

```sh
curl --digest -u admin:password \
  'https://camera/axis-cgi/param.cgi?action=list&group=root.NetBird_VPN'
```

The page reports the management and Signal state, assigned NetBird overlay IP,
and active proxy address. The status endpoint is available through the ACAP
reverse proxy at `/local/NetBird_VPN/api/status`.

## Proxy and security

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
