#!/usr/bin/env bash
# One-time local self-signed code-signing identity for xIssue builds.
#
# macOS TCC keys folder permissions to the app's code signature; rebuilding an
# ad-hoc-signed Go binary changes its cdhash, so every update re-asks for
# Desktop/Documents/Downloads/network-volume access. This creates a stable
# certificate and keychain; `make build` then signs every binary with it.
#
# The trust step shows ONE macOS authorization dialog — approve it once.
set -euo pipefail

[ "$(uname -s)" = "Darwin" ] || { echo "macOS only"; exit 0; }

identity="${XISSUE_CODESIGN_IDENTITY:-xIssue Codesign}"
keychain="$HOME/Library/Keychains/xissue-signing.keychain-db"
pwfile="$HOME/.xissue/codesign.keychain-password"
certfile="$HOME/.xissue/xissue-codesign.crt"


# codesign has no --keychain option: it only searches the user keychain list.
# Add our signing keychain there (idempotent) so the identity is discoverable.
ensure_keychain_in_search_list() {
  local kc_path="$1"
  local -a list=()
  local line
  while IFS= read -r line; do
    line="$(printf '%s' "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')"
    [ -n "$line" ] && list+=("$line")
  done < <(security list-keychains -d user)
  for l in "${list[@]}"; do
    [ "$l" = "$kc_path" ] && return 0
  done
  list+=("$kc_path")
  security list-keychains -d user -s "${list[@]}" >/dev/null
}

identity_ok() {
  security find-identity -v -p codesigning "$keychain" 2>/dev/null | grep -qF "$identity"
}

if identity_ok; then
  echo "✓ signing identity \"$identity\" already ready"
  exit 0
fi

mkdir -p "$HOME/.xissue"
if [ -f "$pwfile" ]; then
  keychain_pw="$(cat "$pwfile")"
else
  keychain_pw="$(openssl rand -hex 24)"
  printf '%s' "$keychain_pw" > "$pwfile"
  chmod 600 "$pwfile"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat > "$work/ext.cnf" <<'EOF'
[req]
distinguished_name=dn
x509_extensions=v3
prompt=no
[dn]
CN=xIssue Codesign
O=xIssue Local
[v3]
basicConstraints=critical,CA:false
keyUsage=critical,digitalSignature
extendedKeyUsage=critical,codeSigning
EOF

openssl req -x509 -newkey rsa:2048 -nodes -keyout "$work/key.pem" -out "$work/cert.pem" \
  -days 3650 -config "$work/ext.cnf" >/dev/null 2>&1
# macOS security cannot read modern PKCS#12 encryption; export legacy (SHA1/3DES).
openssl pkcs12 -export -legacy -macalg sha1 -inkey "$work/key.pem" -in "$work/cert.pem" \
  -out "$work/id.p12" -passout pass:xissue >/dev/null 2>&1

security delete-keychain "$keychain" 2>/dev/null || true
security create-keychain -p "$keychain_pw" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_pw" "$keychain"
security import "$work/id.p12" -k "$keychain" -P xissue -T /usr/bin/codesign >/dev/null
# Let codesign use the private key without another prompt.
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_pw" "$keychain" >/dev/null 2>&1 || true
ensure_keychain_in_search_list "$keychain"
cp "$work/cert.pem" "$certfile"

echo "→ 系统会弹一次授权（security 要求修改信任设置）；请在弹窗中输入密码允许。"
security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$certfile"

if identity_ok; then
  echo "✓ identity ready: $identity"
  echo "  keychain: $keychain"
  echo "  cert:     $certfile"
  echo "  Next build signs automatically; macOS will ask for folder access one last time."
else
  echo "✗ identity still not trusted." >&2
  echo "  Open Keychain Access → \"$identity\" → Get Info → Trust → Code Signing = Always Trust." >&2
  exit 1
fi
