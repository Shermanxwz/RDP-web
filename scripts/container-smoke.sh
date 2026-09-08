#!/usr/bin/env bash
set -euo pipefail
IMAGE="${RDPWEB_IMAGE:-rdp-web:ci}"
VOLUME="rdpweb-ci-data-${GITHUB_RUN_ID:-local}-$$"
NAME="rdp-web-smoke-${GITHUB_RUN_ID:-local}-$$"
BASE="http://127.0.0.1:18080"
COOKIE="${RUNNER_TEMP:-/tmp}/rdpweb-cookie-$$"
RESET_OUT="${RUNNER_TEMP:-/tmp}/rdpweb-reset-$$"
OLD_PASSWORD='correct horse battery staple'
NEW_PASSWORD='new correct horse battery staple'
cleanup() {
  docker rm -f "$NAME" "${NAME}-2" "${NAME}-3" >/dev/null 2>&1 || true
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  rm -f "$COOKIE" "${COOKIE}-2" "${COOKIE}-3" "$RESET_OUT"
}
trap cleanup EXIT

wait_ready() {
  for _ in $(seq 1 40); do
    curl -fsS "$BASE/healthz" >/dev/null && return 0
    sleep 1
  done
  curl -fsS "$BASE/healthz" >/dev/null
}

run_server() {
  local name="$1"
  shift
  docker run -d --name "$name" -p 18080:8080 \
    --read-only --tmpfs /tmp:size=16m,mode=1777 \
    --cap-drop=ALL --security-opt=no-new-privileges:true \
    "$@" -v "$VOLUME:/data" "$IMAGE" >/dev/null
  wait_ready
}

docker volume create "$VOLUME" >/dev/null
run_server "$NAME" -e RDPWEB_SETUP_TOKEN=container-setup-token

setup=$(curl -fsS -c "$COOKIE" -H 'Content-Type: application/json' \
  -d "{\"username\":\"containerowner\",\"password\":\"$OLD_PASSWORD\",\"setupToken\":\"container-setup-token\"}" "$BASE/api/setup")
csrf=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])' <<<"$setup")
curl -fsS -b "$COOKIE" -H "X-RDPWeb-CSRF: $csrf" -H 'Content-Type: application/json' \
  -d '{"id":"container-device","groupId":"","name":"Container Server","host":"container.example.com","port":3389,"username":"Administrator","domain":"","gateway":"","favorite":false,"notes":"","useMultimon":false,"redirectClipboard":true,"audioMode":0}' \
  "$BASE/api/devices" >/dev/null

docker stop "$NAME" >/dev/null
docker rm "$NAME" >/dev/null

run_server "${NAME}-2"
login=$(curl -fsS -c "${COOKIE}-2" -H 'Content-Type: application/json' \
  -d "{\"username\":\"containerowner\",\"password\":\"$OLD_PASSWORD\"}" "$BASE/api/login")
python3 - <<'PY' "$BASE" "${COOKIE}-2"
import json, subprocess, sys
base, cookie = sys.argv[1:]
raw = subprocess.check_output(['curl','-fsS','-b',cookie,base+'/api/devices'])
devices = json.loads(raw)
assert any(d['host'] == 'container.example.com' for d in devices), devices
PY

docker stop "${NAME}-2" >/dev/null
docker rm "${NAME}-2" >/dev/null

# Exercise the exact local/container forgotten-password recovery path against the
# persisted volume. The secret is supplied only over stdin and must never be
# reflected in command output.
printf '%s' "$NEW_PASSWORD" | docker run --rm -i \
  --read-only --tmpfs /tmp:size=16m,mode=1777 \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  -v "$VOLUME:/data" "$IMAGE" reset-password --password-stdin >"$RESET_OUT"
if grep -Fq "$NEW_PASSWORD" "$RESET_OUT" || grep -Fq "$OLD_PASSWORD" "$RESET_OUT"; then
  echo 'password recovery command leaked a password' >&2
  exit 1
fi

run_server "${NAME}-3"
old_status=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -d "{\"username\":\"containerowner\",\"password\":\"$OLD_PASSWORD\"}" "$BASE/api/login")
test "$old_status" = 401
curl -fsS -c "${COOKIE}-3" -H 'Content-Type: application/json' \
  -d "{\"username\":\"containerowner\",\"password\":\"$NEW_PASSWORD\"}" "$BASE/api/login" >/dev/null
python3 - <<'PY' "$BASE" "${COOKIE}-3"
import json, subprocess, sys
base, cookie = sys.argv[1:]
raw = subprocess.check_output(['curl','-fsS','-b',cookie,base+'/api/devices'])
devices = json.loads(raw)
assert any(d['host'] == 'container.example.com' for d in devices), devices
PY
