# ripple

Scheduled ConfigMap and Secret reference checks for Kubernetes.

This chart installs a CronJob that runs the
[ripple](https://github.com/mehrabix/kubectl-ripple) scanner against your
cluster. `check` fails the Job when a workload references a ConfigMap or Secret
that does not exist, so you can alert on configuration rot without anyone
remembering to look.

The scanner is read-only. It never modifies anything.

## Installing

```bash
helm repo add ripple https://mehrabix.github.io/kubectl-ripple
helm repo update
helm install ripple-scan ripple/ripple --namespace ripple-system --create-namespace
```

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `image.repository` | string | `ghcr.io/mehrabix/ripple` | image repository |
| `image.tag` | string | `""` | defaults to `.Chart.AppVersion` |
| `image.pullPolicy` | string | `IfNotPresent` | image pull policy |
| `subcommand` | list | `["check"]` | `check` (exit 1 on a dangling reference) or `orphans` (list unreferenced objects) |
| `args` | list | `["-A"]` | arguments passed to the subcommand |
| `schedule` | string | `0 6 * * *` | cron schedule |
| `suspend` | bool | `false` | suspend the schedule |
| `concurrencyPolicy` | string | `Forbid` | CronJob concurrency policy |
| `startingDeadlineSeconds` | int | `300` | CronJob starting deadline |
| `backoffLimit` | int | `0` | Job backoff limit |
| `activeDeadlineSeconds` | int | `600` | Job active deadline |
| `successfulJobsHistoryLimit` | int | `3` | retained successful Jobs |
| `failedJobsHistoryLimit` | int | `3` | retained failed Jobs |
| `restartPolicy` | string | `Never` | pod restart policy |
| `includeSystemNamespaces` | bool | `false` | also report findings in `kube-*` namespaces |
| `scanSecrets` | bool | `true` | scan Secrets; `false` drops the secret read permission from the Role |
| `serviceAccount.create` | bool | `true` | create a ServiceAccount |
| `serviceAccount.name` | string | `""` | override the ServiceAccount name |
| `serviceAccount.annotations` | object | `{}` | ServiceAccount annotations |
| `rbac.create` | bool | `true` | create the ClusterRole and ClusterRoleBinding |
| `podAnnotations` | object | `{}` | pod annotations |
| `podLabels` | object | `{}` | extra pod labels |
| `priorityClassName` | string | `""` | pod priority class |
| `nodeSelector` | object | `{}` | node selector |
| `tolerations` | list | `[]` | tolerations |
| `affinity` | object | `{}` | affinity |
| `resources` | object | requests `50m`/`64Mi`, limits `500m`/`256Mi` | container resources |
| `podSecurityContext` | object | nonroot uid/gid 65532, `RuntimeDefault` seccomp | pod security context |
| `securityContext` | object | no privilege escalation, read-only root, all capabilities dropped | container security context |

## Examples

Report orphans in every namespace, weekly, without scanning Secrets:

```bash
helm install ripple-orphans ripple/ripple --namespace ripple-system --create-namespace \
  --set subcommand[0]=orphans \
  --set scanSecrets=false
```

Run a scan immediately without waiting for the schedule:

```bash
kubectl create job --from=cronjob/ripple-scan-ripple ripple-now -n ripple-system
kubectl logs -n ripple-system job/ripple-now
```

## Permissions

The ClusterRole grants `get` and `list` on ConfigMaps, Secrets, pods,
replicationcontrollers, deployments, statefulsets, daemonsets, replicasets,
jobs and cronjobs. Nothing is written. Set `scanSecrets=false` to remove the
secret read permission.

## Source

<https://github.com/mehrabix/kubectl-ripple>
