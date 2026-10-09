#!/usr/bin/env bash
# End-to-end test: applies the fixtures to a real cluster, exercises every
# ripple command and asserts on the output.
#
#   BIN=./bin/kubectl-ripple NS=ripple-demo ./test/e2e/run.sh
set -uo pipefail

NS="${NS:-ripple-demo}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${BIN:-$ROOT/bin/kubectl-ripple}"
TMP="$(mktemp -d)"
trap 'kubectl delete namespace "$NS" --wait=false >/dev/null 2>&1; rm -rf "$TMP"' EXIT

fail=0
pass=0

expect() { # description, file, needle
  if grep -qF -- "$3" "$2"; then
    pass=$((pass + 1)); printf '  ok   %s\n' "$1"
  else
    fail=$((fail + 1)); printf '  FAIL %s\n       expected to find: %s\n' "$1" "$3"
    sed 's/^/       | /' "$2"
  fi
}

refute() { # description, file, needle
  if grep -qF -- "$3" "$2"; then
    fail=$((fail + 1)); printf '  FAIL %s\n       should NOT contain: %s\n' "$1" "$3"
    sed 's/^/       | /' "$2"
  else
    pass=$((pass + 1)); printf '  ok   %s\n' "$1"
  fi
}

[ -x "$BIN" ] || { echo "binary not found at $BIN (run: make build)"; exit 2; }

echo "== setting up $NS =="
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -n "$NS" -f "$ROOT/test/e2e/fixtures.yaml" >/dev/null
kubectl -n "$NS" wait --for=condition=Available deployment/web --timeout=90s >/dev/null 2>&1 || true

echo
echo "== who-refs configmap/app-config =="
"$BIN" who-refs configmap/app-config -n "$NS" >"$TMP/who.txt" 2>&1
cat "$TMP/who.txt"
expect "web is a consumer"          "$TMP/who.txt" "Deployment/web"
expect "worker is a consumer"       "$TMP/who.txt" "Deployment/worker"
expect "nightly is a consumer"      "$TMP/who.txt" "CronJob/nightly"
expect "web restarts (envFrom)"     "$TMP/who.txt" "restart required"
refute "statefulset db is not a consumer" "$TMP/who.txt" "StatefulSet/db"
echo

echo "== who-refs secret/db-creds =="
"$BIN" who-refs secret/db-creds -n "$NS" >"$TMP/who2.txt" 2>&1
cat "$TMP/who2.txt"
expect "web reads db-creds" "$TMP/who2.txt" "Deployment/web"
expect "key is reported"    "$TMP/who2.txt" "password"
echo

echo "== subPath volume must not claim live reload =="
"$BIN" who-refs secret/tls-cert -n "$NS" >"$TMP/tls.txt" 2>&1
cat "$TMP/tls.txt"
expect "subPath is detected"       "$TMP/tls.txt" "volume-subPath"
expect "subPath needs a restart"   "$TMP/tls.txt" "restart required"
refute "subPath is not live reload" "$TMP/tls.txt" "live reload"
echo

echo "== orphans =="
"$BIN" orphans -n "$NS" >"$TMP/orphans.txt" 2>&1
cat "$TMP/orphans.txt"
expect "unused-config is an orphan" "$TMP/orphans.txt" "unused-config"
refute "app-config is not an orphan" "$TMP/orphans.txt" "app-config"
refute "db-creds is not an orphan"   "$TMP/orphans.txt" "db-creds"
echo

echo "== impact (only feature-a/feature-c change; log-level untouched) =="
"$BIN" impact configmap/app-config --from-file="$ROOT/test/e2e/app-config-v2.yaml" -n "$NS" >"$TMP/impact.txt" 2>&1
cat "$TMP/impact.txt"
expect "the diff is summarised"        "$TMP/impact.txt" "1 changed"
expect "the added key is summarised"   "$TMP/impact.txt" "1 added"
expect "nightly is unaffected"         "$TMP/impact.txt" "unaffected"
expect "web restarts"                  "$TMP/impact.txt" "restart required"
echo

echo "== check (must find the dangling reference and exit 1) =="
"$BIN" check -n "$NS" >"$TMP/check.txt" 2>&1
rc=$?
cat "$TMP/check.txt"
expect "ghost-secret is reported" "$TMP/check.txt" "ghost-secret"
expect "broken workload is named" "$TMP/check.txt" "Deployment/broken"
if [ "$rc" -eq 1 ]; then pass=$((pass + 1)); printf '  ok   check exits 1 on findings\n'
else fail=$((fail + 1)); printf '  FAIL check exit code was %s, want 1\n' "$rc"; fi
echo

echo "== check must pass once the missing secret exists =="
kubectl -n "$NS" create secret generic ghost-secret --from-literal=token=x >/dev/null
"$BIN" check -n "$NS" >"$TMP/check2.txt" 2>&1
rc2=$?
cat "$TMP/check2.txt"
if [ "$rc2" -eq 0 ]; then pass=$((pass + 1)); printf '  ok   check exits 0 when everything resolves\n'
else fail=$((fail + 1)); printf '  FAIL check exit code was %s, want 0\n' "$rc2"; fi
echo

echo "== json output is valid =="
if "$BIN" orphans -n "$NS" -o json >"$TMP/j.json" 2>&1; then
  if python3 -c "import json,sys; json.load(open('$TMP/j.json'))" 2>/dev/null; then
    pass=$((pass + 1)); printf '  ok   json output parses\n'
  else
    fail=$((fail + 1)); printf '  FAIL json output did not parse\n'; cat "$TMP/j.json"
  fi
else
  fail=$((fail + 1)); printf '  FAIL json run failed\n'; cat "$TMP/j.json"
fi
echo

echo "======================================"
printf 'passed: %d   failed: %d\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
