#!/bin/sh
set -eu

api_pid=
tunnel_pid=
tunnel_token_file=
owned_tunnel_token_file=

terminate() {
  set +e
  if [ -n "$tunnel_pid" ]; then
    kill "$tunnel_pid" 2>/dev/null
  fi
  if [ -n "$api_pid" ]; then
    kill "$api_pid" 2>/dev/null
  fi
  if [ -n "$tunnel_pid" ]; then
    wait "$tunnel_pid" 2>/dev/null
  fi
  if [ -n "$owned_tunnel_token_file" ]; then
    rm -f "$tunnel_token_file"
  fi
  if [ -n "$api_pid" ]; then
    wait "$api_pid" 2>/dev/null
  fi
  exit 143
}

trap terminate INT TERM

if [ -n "${CLOUDFLARED_TOKEN_FILE:-}" ]; then
  tunnel_token_file="$CLOUDFLARED_TOKEN_FILE"
elif [ -n "${CLOUDFLARED_TOKEN:-}" ]; then
  tunnel_token_file="$(mktemp)"
  owned_tunnel_token_file=1
  chmod 600 "$tunnel_token_file"
  printf '%s' "$CLOUDFLARED_TOKEN" > "$tunnel_token_file"
  unset CLOUDFLARED_TOKEN
fi

/usr/local/bin/codex2api &
api_pid=$!

if [ -n "$tunnel_token_file" ]; then
  cloudflared tunnel --no-autoupdate run --token-file "$tunnel_token_file" &
  tunnel_pid=$!
elif [ -n "${CLOUDFLARED_URL:-}" ]; then
  cloudflared tunnel --no-autoupdate --url "$CLOUDFLARED_URL" &
  tunnel_pid=$!
else
  echo "CLOUDFLARED_TOKEN_FILE, CLOUDFLARED_TOKEN, or CLOUDFLARED_URL is not set; running codex2api only" >&2
  wait "$api_pid"
  exit $?
fi

while kill -0 "$api_pid" 2>/dev/null && kill -0 "$tunnel_pid" 2>/dev/null; do
  sleep 2
done

set +e
wait "$api_pid"
api_status=$?
wait "$tunnel_pid"
tunnel_status=$?

kill "$api_pid" "$tunnel_pid" 2>/dev/null

if [ -n "$owned_tunnel_token_file" ]; then
  rm -f "$tunnel_token_file"
fi

if [ "$api_status" -ne 0 ]; then
  exit "$api_status"
fi
exit "$tunnel_status"
