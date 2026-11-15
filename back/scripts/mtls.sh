#!/usr/bin/env bash
set -euo pipefail
umask 077
cd "$(dirname "$0")/.."
cert_root="${MTLS_CERT_ROOT:-secrets/mtls}"
mkdir -p "$cert_root"
chmod 700 "$cert_root"
if [[ ! -f "$cert_root/ca.key" ]]; then
    [[ ! -f "$cert_root/ca.pem" ]] || { echo "CA key missing; refusing to replace an existing CA" >&2; exit 1; }
    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$cert_root/ca.key" 2>/dev/null
    openssl req -x509 -new -key "$cert_root/ca.key" -sha256 -days 1825 \
        -out "$cert_root/ca.pem" -subj /CN=ConfHub-Internal-CA \
        -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign'
fi
chmod 600 "$cert_root/ca.key"
chmod 644 "$cert_root/ca.pem"
for name in identity-svc conference-svc application-svc engagement-svc operations; do
    mkdir -p "$cert_root/$name"
    chmod 750 "$cert_root/$name"
    if [[ -f "$cert_root/$name/cert.pem" ]] && openssl x509 -checkend 2592000 -noout -in "$cert_root/$name/cert.pem" >/dev/null; then
        continue
    fi
    scratch="$(mktemp -d "$cert_root/.issuance.XXXXXX")"
    trap 'case "$scratch" in "$cert_root"/.issuance.*) rm -rf -- "$scratch" ;; esac' EXIT
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out "$scratch/key.pem" 2>/dev/null
    openssl req -new -key "$scratch/key.pem" -out "$scratch/request.pem" -subj "/CN=$name"
    usage=serverAuth,clientAuth
    [[ "$name" != operations ]] || usage=clientAuth
    extensions="$(printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=%s\nsubjectAltName=DNS:%s\n' "$usage" "$name")"
    openssl x509 -req -in "$scratch/request.pem" -CA "$cert_root/ca.pem" -CAkey "$cert_root/ca.key" \
        -set_serial "0x$(openssl rand -hex 16)" -days 90 -sha256 -out "$scratch/cert.pem" \
        -extfile <(printf '%s\n' "$extensions") 2>/dev/null
    chmod 640 "$scratch/key.pem"
    chmod 644 "$scratch/cert.pem"
    if [[ "$(id -u)" == 0 ]]; then chgrp 0 "$cert_root/$name" "$scratch/key.pem"; fi
    mv "$scratch/key.pem" "$cert_root/$name/key.pem"
    mv "$scratch/cert.pem" "$cert_root/$name/cert.pem"
    echo "Internal certificate issued: $name"
    rm -rf -- "$scratch"
    trap - EXIT
done
