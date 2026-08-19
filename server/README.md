# Local NetBird server

LAN-only development server for the Axis ACAP test camera.

- Management, signal, and relay: `http://192.168.1.1:8080`
- STUN: `192.168.1.1:3478/udp`
- Persistent data: Podman volume `netbird_data`

This stack intentionally uses plain HTTP and a fixed LAN address. It is for
local testing only and must not be exposed to the Internet.
