# Contributing

## Development setup

The package is built with Docker or Podman and the Axis ACAP Native SDK image.
Go tests can run locally with:

```sh
go test ./...
```

Build both ACAP4 architectures with:

```sh
./build.sh
```

Build one architecture with:

```sh
ARCHES=aarch64 ./build.sh
```

Do not commit setup keys, personal access tokens, camera passwords, generated
`.eap` files, or local NetBird server data.

## Pull requests

Keep changes focused, update `CHANGELOG.md` when behavior changes, and include
the camera model and AXIS OS version when reporting hardware results. Verify the
userspace client still works without root privileges or `/dev/net/tun`.
