# talos-vip-watcher

A tiny service for Talos control plane nodes that binds a virtual IP to a dummy interface while
the node's Kubernetes API server is ready, and withdraws it while it is not. A BGP speaker that
exports the interface's addresses then advertises the VIP from healthy control plane nodes only,
which gives an anycast API endpoint without a load balancer.

It runs as a Talos `ContainerConfig`: no Kubernetes, so it works before the cluster does.

## How it works

Every `--interval` the watcher requests `--url` (by default `https://127.0.0.1:6443/readyz`).
HTTP 200 is healthy; anything else, including a timeout, is not.

- The VIP is bound after `--threshold` consecutive healthy checks, and withdrawn after
  `--threshold` consecutive failing ones, so a single blip does not make it flap.
- Binding creates the dummy interface `--iface` if needed, brings it up and adds the VIP.
  Withdrawing removes the VIP and takes the interface down.
- The desired state is applied on every check, so an address removed by someone else comes back.
- The watcher starts withdrawn, so a VIP left behind by a crashed instance is removed until the
  API server proves healthy again, and it withdraws the VIP when it receives SIGTERM or SIGINT.
- It only ever manages a `dummy` interface. Naming an existing interface of another type is
  refused, since withdrawing takes the interface down.
- IPv4 and IPv6 VIPs are supported.

The watcher speaks no BGP itself. See the requirements below.

## Requirements

1. **A BGP speaker that exports the VIP.** Something on the node has to advertise the address
   bound to `--iface`, for example a routing daemon configured to export the connected `/32`
   (or `/128`) route of that interface. When the VIP is withdrawn the route disappears, and so
   does the advertisement.

2. **Anonymous access to `/readyz`.** The check sends no credentials. With Talos 1.14's
   `KubeAPIServerConfig` document, the default `KubeAuthenticationConfig` allows anonymous requests
   to `/livez`, `/readyz` and `/healthz`, so the default URL works. With the legacy
   `cluster.apiServer` section of the v1alpha1 config, Talos runs the API server with
   `--anonymous-auth=false`: `/readyz` answers 401, the watcher logs "anonymous access to the health
   endpoint is not allowed" and never binds the VIP.

3. **The VIP excluded from the node's own addresses.** Talos counts addresses on dummy interfaces
   as node addresses, so it could pick the VIP as the kubelet node IP or as etcd's advertised
   address. Exclude it explicitly:

   ```yaml
   machine:
     kubelet:
       nodeIP:
         validSubnets:
           - 10.1.0.0/16
           - "!10.1.1.100/32"
   cluster:
     etcd:
       advertisedSubnets:
         - 10.1.0.0/16
         - "!10.1.1.100/32"
   ```

4. **Root with `NET_ADMIN`.** The image runs as root. Do not set `runAs` to another user: the
   added capability does not survive the exec of a non-root process.

## Talos configuration

```yaml
apiVersion: v1alpha1
kind: ContainerConfig
name: talos-vip-watcher
image: ghcr.io/sidero-community/talos-vip-watcher:latest
args:
  - --vip=10.1.1.100/32
  - --iface=vip-api
  - --url=https://127.0.0.1:6443/readyz
  - --interval=2s
network:
  mode: host
security:
  capabilities:
    add:
      - NET_ADMIN
dependsOn:
  networks:
    - addresses
```

`network.mode: host` puts the dummy interface in the node's network namespace, where the BGP
speaker sees it. The default `restricted` security profile drops every capability, and
`NET_ADMIN` adds back the only one the watcher needs.

Talos warns that `:latest` is not digest-pinned and may change when the container restarts.
For production, pin the image by digest, or use a version tag once one is published:

```yaml
image: ghcr.io/sidero-community/talos-vip-watcher@sha256:<digest>
```

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--vip` | required | VIP in CIDR notation, e.g. `10.1.1.100/32` |
| `--iface` | `vip-api` | dummy interface to bind the VIP to; created if missing |
| `--url` | `https://127.0.0.1:6443/readyz` | health check URL; healthy means HTTP 200 |
| `--interval` | `2s` | time between checks |
| `--timeout` | `2s` | timeout of one check |
| `--threshold` | `2` | consecutive results needed to bind or withdraw the VIP |
| `--insecure` | `true` | skip TLS verification; the API server's certificate is not issued by a CA the container knows |

## Image

`ghcr.io/sidero-community/talos-vip-watcher` is built for `linux/amd64` and `linux/arm64` from
`scratch` with only the CA bundle and the static binary. CI publishes:

- `latest` and `sha-<commit>` from `main`
- `X.Y.Z` and `X.Y` from `vX.Y.Z` tags

## Development

```sh
make test        # unit tests
make test-netns  # netlink tests in a throwaway network namespace (unshare -rn)
make lint
make image       # multi-arch build with docker buildx
```

The netlink tests create and delete interfaces, so they refuse to run unless
`TALOS_VIP_WATCHER_NETNS=1` is set, which `make test-netns` does inside a new network namespace.
