#!/usr/bin/env bash
# Generate the krew plugin manifest for a released version from the checksums
# published by goreleaser.
#
#   ./hack/update-krew-manifest.sh v0.1.0 [--check]
#
# --check exits 1 when the committed manifest does not match the release, which
# makes it usable as a CI guard.
set -euo pipefail

VERSION="${1:?usage: update-krew-manifest.sh v0.1.0 [--check]}"
MODE="${2:-write}"
REPO="${REPO:-mehrabix/kubectl-ripple}"
URI_BASE="https://github.com/${REPO}/releases/download/${VERSION}"
OUT="$(cd "$(dirname "$0")/.." && pwd)/deploy/krew/ripple.yaml"

semver="${VERSION#v}"

sums="$(mktemp)"
trap 'rm -f "$sums" "$sums.new"' EXIT
curl -fsSL "${URI_BASE}/checksums.txt" -o "$sums"

entry() { # os arch ext exe
  local os="$1" arch="$2" ext="$3" exe="${4:-}" name sha
  name="kubectl-ripple_${semver}_${os}_${arch}.${ext}"
  sha="$(awk -v n="$name" '$2 == n {print $1}' "$sums")"
  if [ -z "$sha" ]; then
    echo "missing checksum for ${name}" >&2
    exit 1
  fi
  cat <<EOF
    - selector:
        matchLabels:
          os: ${os}
          arch: ${arch}
      uri: ${URI_BASE}/${name}
      sha256: ${sha}
      bin: kubectl-ripple${exe}
EOF
}

cat >"$sums.new" <<EOF
apiVersion: krew.googlecontainertools.github.com/v1alpha2
kind: Plugin
metadata:
  name: ripple
spec:
  version: ${VERSION}
  homepage: https://github.com/${REPO}
  shortDescription: See what a ConfigMap or Secret change will break
  description: |
    ripple maps the ConfigMaps and Secrets that workloads consume and reports
    what a change or a delete will actually do: which workloads restart, which
    reload in place, which are unaffected, and which objects nothing references.

    Read-only, deterministic and offline. No agent, no LLM.
  caveats: |
    ripple never modifies your cluster. It needs get/list on ConfigMaps,
    Secrets and the pod-template workload kinds.
  platforms:
$(entry linux amd64 tar.gz)
$(entry linux arm64 tar.gz)
$(entry darwin amd64 tar.gz)
$(entry darwin arm64 tar.gz)
$(entry windows amd64 zip .exe)
$(entry windows arm64 zip .exe)
EOF

if [ "$MODE" = "--check" ]; then
  if ! diff -u "$OUT" "$sums.new"; then
    echo "krew manifest is out of date; run ./hack/update-krew-manifest.sh ${VERSION}" >&2
    exit 1
  fi
  echo "krew manifest is up to date"
else
  mv "$sums.new" "$OUT"
  echo "wrote $OUT"
fi
