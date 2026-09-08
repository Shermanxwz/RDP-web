#!/usr/bin/env bash
set -euo pipefail
IMAGE="${RDPWEB_IMAGE:-rdp-web:ci}"
VOLUME="rdpweb-ci-data-${GITHUB_RUN_ID:-local}-$$"
NAME="rdp-web-smoke-${GITHUB_RUN_ID:-local}-$$"
BASE="http://127.0.0.1:18080"
COOKIE="${RUNNER_TEMP:-/tmp}/rdpweb-cookie-$$"
cleanup() {
  docker rm -f "$NAME" "${NAME}-2" >/dev/null 2>&1 || true
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  rm -f "$COOKIE" "${COOKIE}-2"
}
trap cleanup EXIT

docker volume create "$VOLUME" >/dev/null
docker run -d --name "$NAME" -p 18080:8080 \
  --read-only --tmpfs /tmp:size=16m,mode=1777 \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  -e RDPWEB_SETUP_TOKEN=container-setup-token \
  -v "$VOLUME:/data" "$IMAGE" >/dev/null
for _ in $(seq 1 40); do
  curl -fsS "$BASE/healthz" >/dev/null && break
  sleep 1
done
curl -fsS "$BASE/healthz" >/dev/null
setup=$(curl -fsS -c "$COOKIE" -H 'Content-Type: application/json' \
  -d '{"username":"containerowner","password":"correct horse battery staple","setupToken":"container-setup-token"}' "$BASE/api/setup")
csrf=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])' <<<"$setup")
curl -fsS -b "$COOKIE" -H "X-RDPWeb-CSRF: $csrf" -H 'Content-Type: application/json' \
  -d '{"id":"container-device","groupId":"","name":"Container Server","host":"container.example.com","port":3389,"username":"Administrator","domain":"","gateway":"","favorite":false,"notes":"","useMultimon":false,"redirectClipboard":true,"audioMode":0}' \
  "$BASE/api/devices" >/dev/null

docker stop "$NAME" >/dev/null
docker rm "$NAME" >/dev/null

docker run -d --name "${NAME}-2" -p 18080:8080 \
  --read-only --tmpfs /tmp:size=16m,mode=1777 \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  -v "$VOLUME:/data" "$IMAGE" >/dev/null
for _ in $(seq 1 40); do
  curl -fsS "$BASE/healthz" >/dev/null && break
  sleep 1
done
curl -fsS "$BASE/healthz" >/dev/null
login=$(curl -fsS -c "${COOKIE}-2" -H 'Content-Type: application/json' \
  -d '{"username":"containerowner","password":"correct horse battery staple"}' "$BASE/api/login")
python3 - <<'PY' "$BASE" "${COOKIE}-2"
import json, subprocess, sys
base, cookie = sys.argv[1:]
raw = subprocess.check_output(['curl','-fsS','-b',cookie,base+'/api/devices'])
devices = json.loads(raw)
assert any(d['host'] == 'container.example.com' for d in devices), devices
PY
