#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
RUNTIME=${RUNTIME:-}
ARCHES=${ARCHES:-"aarch64 armv7hf"}

if [ -z "$RUNTIME" ]; then
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
        RUNTIME=docker
    elif command -v podman >/dev/null 2>&1; then
        RUNTIME=podman
    elif command -v docker >/dev/null 2>&1; then
        RUNTIME=docker
    else
        echo "Error: neither Docker nor Podman is available" >&2
        exit 1
    fi
fi

rm -rf "$REPO_ROOT/releases"
mkdir -p "$REPO_ROOT/releases"

for arch in $ARCHES; do
    tag="netbird-acap-${arch}"
    output_dir="$REPO_ROOT/.build/${arch}"
    echo "==> Building ${arch} with ${RUNTIME}"
    rm -rf "$output_dir"
    mkdir -p "$output_dir"
    "$RUNTIME" build --build-arg ARCH="$arch" --target package -t "$tag" "$REPO_ROOT"
    container=$($RUNTIME create "$tag")
    "$RUNTIME" cp "$container:/opt/app/." "$output_dir/"
    "$RUNTIME" rm "$container" >/dev/null
    find "$output_dir" -type f -name "*.eap" -exec mv {} "$REPO_ROOT/releases/" \;
done

rm -rf "$REPO_ROOT/.build"

ls -lh "$REPO_ROOT"/releases/*.eap
