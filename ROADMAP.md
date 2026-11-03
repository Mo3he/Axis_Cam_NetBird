# Roadmap

Planned work for the NetBird VPN ACAP. Items are ideas and intentions, not
commitments, and nothing here has a fixed release date. Shipped items move to
[CHANGELOG.md](CHANGELOG.md).

## Planned

### Reset NetBird identity

**Problem.** The UI cannot wipe a camera's NetBird identity. The private key
in `localdata/config.json` is reused on every start (`persistedPrivateKey()` in
[app/main.go](app/main.go)). Moving a camera to another account or network, or
recovering from a revoked peer, currently means reinstalling the ACAP.

**Proposal.**

- Add a **Reset identity** action, with a confirmation dialog, in the web UI
  and the settings endpoint.
- On reset, stop the client, remove `config.json` and `state.json` from the
  state directory, and restart. The app then waits for a new setup key.
- Keep the current "disconnect" behaviour separate, so the identity is kept
  unless the user explicitly resets it.

**Notes.** The embedded client (`client/embed`) has no deregister or logout
call. The old peer therefore stays in the NetBird dashboard until it is removed
there or expires, so the UI should say so.

### DNS fallback for management and signal bootstrap

**Problem.** Cameras are often placed on VLANs with no DNS server, a broken
one, or one that blocks public names. The client then cannot resolve the
management, signal or relay hostnames, and it never connects, even though
outbound HTTPS works.

**Proposal.**

- Install a fallback resolver in-process before `client.Start()`: replace
  `net.DefaultResolver` with a Go resolver that tries the system DNS first and,
  on failure or timeout, resolves the name over DNS-over-HTTPS (for example
  `cloudflare-dns.com` and `dns.google`, reached by IP address).
- Add an opt-in setting (`DNSFallback`: `off` / `doh`, default `off`), and log
  when the fallback was used. Blocked DNS is often a deliberate network policy,
  so the camera must not bypass it unless an administrator asks it to. A
  configurable DNS server is a simpler alternative worth considering.
- Show in the status API whether bootstrap had to use the fallback.

**Notes.** NetBird 0.82.0 does not replace `net.DefaultResolver` itself, so an
override in [app/main.go](app/main.go) should apply to its management and signal
dials without patching upstream. Check this on a camera with DNS blocked before
shipping. The outbound proxies' system DNS fallback in
[app/dial.go](app/dial.go) uses `net.DefaultResolver` too, so it would gain the
same fallback. Another commercial NetBird ACAP solves the same problem with a
separate DNS-over-HTTPS proxy and a patched NetBird client, which shows that the problem
occurs in real deployments.

## Under consideration

- **Keep the setup key out of the environment.** [app/NetBird_VPN_run](app/NetBird_VPN_run)
  currently exports `NB_SETUP_KEY`, which can be read from `/proc/<pid>/environ`
  until enrollment succeeds and the key is cleared. A 0600 file that is deleted
  after it has been read would close that window. The risk is low, because the
  file is only readable by the same user and the key is cleared after
  enrollment.
- **Legacy ARTPEC-5 (MIPS) build.** Older cameras need a `package.conf`-only
  package with no manifest, reverse proxy or FastCGI. Control would go through
  `param.conf` parameters (for example an `Action` enum) polled by the app, and
  status would be published as static files under `html/`. The Go client would
  need a 32-bit MIPS build (`GOARCH=mipsle GOMIPS=softfloat`), so memory and
  flash limits must be checked first. This may belong in the shared ACAP
  standards rather than in this repository alone.
