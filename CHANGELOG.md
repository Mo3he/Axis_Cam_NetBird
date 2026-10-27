# Changelog

## 0.81.5 - 2026-10-27

- Update to upstream 0.81.5.

## 0.81.4 - 2026-10-26

- Update to upstream 0.81.4.
- Rebuilt with Go 1.27.3 and newer golang.org/x/net and golang.org/x/crypto,
  fixing reachable vulnerabilities (GO-2026-6809, GO-2026-6603, GO-2026-6610,
  GO-2026-6611, GO-2026-6612, GO-2026-6617, GO-2026-6354, GO-2026-6355).

## 0.81.3 - 2026-10-20

- Update to upstream 0.81.3.

## 0.81.2 - 2026-10-17

- Update to upstream 0.81.2.

## 0.81.1 - 2026-10-16

- Update to upstream 0.81.1.

## 0.80.0 - 2026-10-02

- Update to upstream 0.80.0.

## 0.79.0 - 2026-09-19

- Update to upstream 0.79.0.

## 0.78.2 - 2026-09-15

- Update to upstream 0.78.2.

## 0.78.1 - 2026-09-04

- Update to upstream 0.78.1.

## 0.77.1 - 2026-08-22

- Update to upstream 0.77.1.

## Unreleased

- Fixed settings not loading or saving on devices without `/axis-cgi/param.cgi`,
  such as recorder/NVR- and access-control-class devices.
- Added a settings endpoint at `/local/NetBird_VPN/config/settings` that the
  settings page falls back to when `param.cgi` is unavailable. It is served by
  the parameter bridge so the setup key can be entered before enrollment.
- Fixed the settings page showing a raw `Unexpected token '<'` JSON error before
  a setup key is entered. It now reports `Not running` and prompts for a key.

## 0.77.0

- Updated the embedded NetBird client to v0.77.0.
- Aligned the package version with the bundled NetBird release so new upstream
  releases are built and published automatically.
- Added inbound port forwarding so the camera is reachable on its NetBird IP,
  with a configurable `ForwardPorts` list defaulting to `80,443,554`.
- Added an inbound SOCKS5 proxy on the NetBird IP, port `1080` by default, for
  reaching any camera port from the overlay.

## 0.1.2

- Added a configurable loopback SOCKS5 proxy, default `127.0.0.1:11080`, with a
  new `Socks5Port` parameter.
- Added SOCKS5 reporting to the settings page and the status API.
- Added a light/dark theme toggle that persists across sessions.
- Added a live service log viewer with severity highlighting and scroll control.
- Added a connection banner showing connected, connecting, or stopped state.
- Cleared the stored setup key automatically after successful enrollment.
- Reused the persisted NetBird identity on restart so the client reconnects once
  the setup key has been cleared.

## 0.1.1

- Added the ACAP settings and connection-status page.
- Added a status endpoint reporting management, Signal, and overlay state.
- Added aarch64 and armv7hf package builds and CI packaging.
- Changed the default loopback HTTP proxy port to `18080` to avoid common ACAP
  port collisions.
- Added a LAN-only self-hosted NetBird server configuration for testing.

## 0.1.0

- Initial userspace NetBird ACAP prototype.
- Verified on AXIS OS 13 aarch64 hardware.
- Uses NetBird embedded client netstack mode without root, `/dev/net/tun`, or kernel WireGuard.
