# Third-party notices

This package contains the NetBird embedded client and its dependencies. The
packaging code in this repository is licensed under the BSD 3-Clause License;
upstream components retain their own licenses.

## NetBird

- Project: <https://github.com/netbirdio/netbird>
- License: BSD 3-Clause for the client components used here.
- Version: v0.74.3.

## WireGuard userspace implementation

- Project: <https://github.com/netbirdio/wireguard-go>
- License: MIT.
- Used through NetBird's userspace WireGuard and netstack implementation.

## gVisor netstack

- Project: <https://gvisor.dev/>
- License: Apache License 2.0.

## Go dependencies

- **github.com/things-go/go-socks5** — MIT license —
  <https://github.com/things-go/go-socks5>.

The complete dependency and license metadata is recorded in `go.mod` and
`go.sum`. This package is built with `CGO_ENABLED=0`; it does not link against
system libraries for the Go daemon.

## Trademarks

NetBird and WireGuard are trademarks of their respective owners. This is an
independent community project and is not affiliated with or endorsed by NetBird,
WireGuard, or Axis Communications.
