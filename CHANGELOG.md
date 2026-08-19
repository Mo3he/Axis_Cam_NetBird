# Changelog

## Unreleased

- Added the ACAP settings and connection-status page.
- Added a NetBird management, Signal, and relay status endpoint.
- Added aarch64 and armv7hf package builds.
- Added a LAN-only self-hosted NetBird server configuration for testing.

## 0.1.1

- Added the settings and connection-status page.
- Added ACAP4 aarch64 and armv7hf release builds and CI packaging.
- Changed the default loopback HTTP proxy port to `18080` to avoid common ACAP
  port collisions.
- Added a configurable loopback SOCKS5 proxy on port `11080` by default.

## 0.1.2

- Added the loopback SOCKS5 proxy and configurable `Socks5Port` parameter.
- Added SOCKS5 status reporting to the settings page and status API.

## 0.1.0

- Initial userspace NetBird ACAP prototype.
- Verified on AXIS OS 13 aarch64 hardware.
- Uses NetBird embedded client netstack mode without root, `/dev/net/tun`, or kernel WireGuard.
