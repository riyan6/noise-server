#!/usr/bin/env bash
set -euo pipefail

ADMIN_USER="${NOISE_ADMIN_USERNAME:-admin}"
ADMIN_PASS="${NOISE_ADMIN_PASSWORD:-}"
ADDR="${NOISE_ADDR:-127.0.0.1:8080}"
API_URL="${NOISE_API_URL:-https://noise-api.nuox.de}"
DASHBOARD_URL="${NOISE_DASHBOARD_URL:-https://noise.nuox.de}"
ADMIN_ORIGINS="${NOISE_ADMIN_ORIGINS:-https://noise-admin.nuox.de}"
PUBLIC_ORIGINS="${NOISE_PUBLIC_ORIGINS:-https://noise.nuox.de}"
COOKIE_SECURE="${NOISE_COOKIE_SECURE:-true}"
TIMEZONE="${NOISE_TIMEZONE:-Asia/Shanghai}"
GITHUB_REPO="${NOISE_SERVER_REPO:-riyan6/noise-server}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--password)
      ADMIN_PASS="${2:-}"
      shift 2
      ;;
    -u|--username)
      ADMIN_USER="${2:-admin}"
      shift 2
      ;;
    --addr)
      ADDR="${2:-127.0.0.1:8080}"
      shift 2
      ;;
    --api-url)
      API_URL="${2:-}"
      shift 2
      ;;
    --dashboard-url)
      DASHBOARD_URL="${2:-}"
      shift 2
      ;;
    --admin-origins)
      ADMIN_ORIGINS="${2:-}"
      shift 2
      ;;
    --public-origins)
      PUBLIC_ORIGINS="${2:-}"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [[ ! -f /etc/noise/noise.env && ( ${#ADMIN_PASS} -lt 12 || ${#ADMIN_PASS} -gt 72 ) ]]; then
  echo "Error: first install requires -p <password> (12 to 72 characters)." >&2
  exit 1
fi

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Error: unsupported architecture: $ARCH_RAW" >&2
    exit 1
    ;;
esac

BINARY_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/noise-server-linux-${ARCH}"
TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT

echo "==> Downloading noise-server (${ARCH}) from ${BINARY_URL} ..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BINARY_URL" -o "$TMP_BIN"
else
  wget -qO "$TMP_BIN" "$BINARY_URL"
fi

id -u noise >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin noise
mkdir -p /opt/noise /etc/noise
install -m 0755 -o noise -g noise "$TMP_BIN" /opt/noise/noise-server

if [[ ! -f /etc/noise/noise.env || -n "$ADMIN_PASS" ]]; then
  cat > /etc/noise/noise.env <<EOF
NOISE_ADMIN_USERNAME=${ADMIN_USER}
NOISE_ADMIN_PASSWORD=${ADMIN_PASS}
NOISE_ADDR=${ADDR}
NOISE_DB=/var/lib/noise/noise.db
NOISE_API_URL=${API_URL}
NOISE_DASHBOARD_URL=${DASHBOARD_URL}
NOISE_ADMIN_ORIGINS=${ADMIN_ORIGINS}
NOISE_PUBLIC_ORIGINS=${PUBLIC_ORIGINS}
NOISE_COOKIE_SECURE=${COOKIE_SECURE}
NOISE_TIMEZONE=${TIMEZONE}
NOISE_TRUSTED_PROXIES=127.0.0.1,::1
EOF
  chown root:noise /etc/noise/noise.env
  chmod 0640 /etc/noise/noise.env
fi

cat > /etc/systemd/system/noise-server.service <<'EOF'
[Unit]
Description=Noise monitoring API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=noise
Group=noise
WorkingDirectory=/opt/noise
EnvironmentFile=/etc/noise/noise.env
StateDirectory=noise
StateDirectoryMode=0700
ExecStart=/opt/noise/noise-server
Restart=on-failure
RestartSec=5
TimeoutStopSec=15
UMask=0077

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable noise-server.service
systemctl restart noise-server.service

echo "==> noise-server started successfully."
systemctl --no-pager --full status noise-server.service || true
