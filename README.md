<p align="center">
  <h1 align="center">ripple</h1>
  <p align="center"><strong>See the ripple before you make the change.</strong></p>
  <p align="center">
    <a href="https://github.com/mehrabix/kubectl-ripple/actions/workflows/ci.yml"><img alt="ci" src="https://github.com/mehrabix/kubectl-ripple/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://github.com/mehrabix/kubectl-ripple/releases/latest"><img alt="release" src="https://img.shields.io/github/v/release/mehrabix/kubectl-ripple"></a>
    <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
    <img alt="read-only" src="https://img.shields.io/badge/cluster%20access-read--only-brightgreen">
  </p>
</p>

`ripple` is a kubectl plugin that maps the ConfigMaps and Secrets your workloads
consume and tells you what a change actually does:

- which workloads **restart**,
- which ones **reload in place**,
- which ones **do not care** about the keys you touched,
- and which ConfigMaps and Secrets **nothing references at all**.

It is read-only, deterministic, and offline. No LLM, no agent in your cluster,
no data leaving the environment — just the object graph the API server already
knows about.

## Why

Updating a ConfigMap does not restart anything, and Kubernetes has no built-in
answer for which workloads depend on which configuration.
[kubernetes/kubernetes#22368](https://github.com/kubernetes/kubernetes/issues/22368)
— *Facilitate ConfigMap rollouts / management* — is the most upvoted issue in
the project's history (1,892 reactions) and it has been open since March 2016.

Workarounds exist, but they answer the wrong question. Reloader restarts
workloads *after* the fact but cannot tell you what is about to happen.
`kubectl graph` draws the resources that exist; it does not model how a
reference behaves. So the answer to *"can I safely delete this Secret?"* is
still a careful reading of every manifest in the namespace.

`ripple` answers that question directly.

## The part that matters: how a reference behaves

Not all references are equal, and the difference decides whether you need a
rollout.

| How it is consumed | On change | Why |
|---|---|---|
| `env[].valueFrom.configMapKeyRef` / `secretKeyRef` | **restart required** | the value is snapshotted when the container starts |
| `envFrom[]` | **restart required** | same, for every key |
| `volumes[].configMap` / `.secret` | live reload | kubelet refreshes the projected files in place |
| `volumes[].projected` | live reload | same |
| a volume mounted with **`subPath`** | **restart required** | subPath mounts are not refreshed — the classic *"I changed the ConfigMap and nothing happened"* |
| `imagePullSecrets` | new pods only | evaluated when the image is pulled |

## Install

```bash
# krew
kubectl krew install ripple

# homebrew
brew install mehrabix/tap/ripple

# go
go install github.com/mehrabix/kubectl-ripple/cmd/kubectl-ripple@latest
```

Prebuilt binaries for linux, macOS and Windows (amd64/arm64) are on the
[releases page](https://github.com/mehrabix/kubectl-ripple/releases).

## Usage

### `who-refs` — who depends on this, and will it restart?

```console
$ kubectl ripple who-refs configmap/app-config -n prod
ConfigMap/app-config is referenced by 3 workloads (2 restart required, 1 live reload)

NAMESPACE  WORKLOAD           MODE     EFFECT            CONTAINER  KEYS
prod       CronJob/nightly    env      restart required  job        log-level
prod       Deployment/web     envFrom  restart required  app        *
prod       Deployment/worker  volume   live reload       -          *
```

### `impact` — what will this change do?

Give it the new manifest and it compares it key by key against the live object.
A workload that reads only keys you did not touch is reported as unaffected.

```console
$ kubectl ripple impact configmap/app-config --from-file=app-config.yaml -n prod
ConfigMap/app-config (namespace prod)

  1 changed, 1 added

NAMESPACE  WORKLOAD           MODE     EFFECT            KEYS
prod       CronJob/nightly    env      unaffected        -
prod       Deployment/web     envFrom  restart required  *
prod       Deployment/worker  volume   live reload       *
```

`nightly` reads a single key that did not change, so it will not restart.
`web` consumes the whole ConfigMap as environment variables, so it must.

### `orphans` — what does nothing use?

```console
$ kubectl ripple orphans -n prod
NAMESPACE  KIND       NAME
prod       ConfigMap  unused-config
prod       Secret     legacy-api-token

2 unreferenced objects in scope
```

Service account tokens, Helm release records, ArgoCD bookkeeping and
`kube-root-ca.crt` are ignored — only your configuration shows up.

### `check` — fail before it breaks

A typo in a reference name produces a pod that never starts. `check` turns that
into an exit code, so it works as a CI or pre-deploy gate:

```console
$ kubectl ripple check -n prod
NAMESPACE  WORKLOAD         MISSING REFERENCE      MODE
prod       Deployment/oops  Secret/does-not-exist  env

1 dangling reference
$ echo $?
1
```

All commands accept `-o json` for scripting, plus the usual `-n`, `-A`,
`--context` and `--kubeconfig`.

## Use it in CI

```yaml
- name: verify every reference resolves
  run: |
    kubectl krew install ripple
    kubectl ripple check -A
```

## Run it on a schedule

The Helm chart installs a CronJob that scans the cluster on a schedule and
fails the Job when it finds something, so you can alert on configuration rot
without anyone remembering to look.

```bash
helm repo add ripple https://mehrabix.github.io/kubectl-ripple
helm repo update
helm install ripple-scan ripple/ripple \
  --namespace ripple-system --create-namespace
```

The chart is also published to `oci://ghcr.io/mehrabix/charts/ripple`.

| Value | Default | Description |
|---|---|---|
| `subcommand` | `["check"]` | `check` or `orphans` |
| `args` | `["-A"]` | arguments for the subcommand |
| `schedule` | `0 6 * * *` | cron schedule |
| `scanSecrets` | `true` | scan Secrets; `false` drops the secret read permission from the Role |
| `includeSystemNamespaces` | `false` | also report findings in `kube-*` namespaces |

See [deploy/helm/ripple](deploy/helm/ripple) for the full set of values.

## Permissions

`ripple` only ever reads. It needs `get` and `list` on ConfigMaps, Secrets and
the pod-template workloads (`deployments`, `statefulsets`, `daemonsets`,
`replicasets`, `replicationcontrollers`, `jobs`, `cronjobs`, `pods`).

It never prints Secret values. The Helm chart lets you drop the secret read
permission entirely with `--set scanSecrets=false`.

## Non-goals

- **It does not change anything.** There is no `--fix`. Restarting your
  workloads is a decision with a blast radius of its own; ripple tells you what
  that blast radius is so you can decide.
- **It does not call an LLM.** The graph is derived from the API objects. The
  same input always produces the same output.
- Custom resources that embed a pod template are not inspected.

## Development

```bash
make build     # build bin/kubectl-ripple
make test      # unit tests
make e2e       # end-to-end test against the current kube-context
make lint      # gofmt + go vet
```

The end-to-end test applies [test/e2e/fixtures.yaml](test/e2e/fixtures.yaml) to
a throwaway namespace, covers every reference mode, and asserts on the output of
all four commands.

## License

[Apache-2.0](LICENSE)
