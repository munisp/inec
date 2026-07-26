#!/usr/bin/env python3
"""Render APISIX standalone configuration with a protected device mTLS SSL resource."""

from __future__ import annotations

import os
import pathlib
import re
import shutil
import subprocess
import sys


BASE_DIR = pathlib.Path(os.getenv("APISIX_CONFIG_INPUT_DIR", "/input"))
OUTPUT_DIR = pathlib.Path(os.getenv("APISIX_CONFIG_OUTPUT_DIR", "/output"))


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"missing required environment variable: {name}")
    return value


def pem_from_env_path(name: str, marker: str, command: list[str]) -> str:
    value = required(name)
    path = pathlib.Path(value)
    if not path.is_file():
        raise RuntimeError(f"{name} does not name a readable file")
    content = path.read_text(encoding="utf-8").strip()
    if marker not in content:
        raise RuntimeError(f"{name} is not a valid {marker} PEM file")
    try:
        subprocess.run(command + ["-in", str(path), "-noout"], check=True, capture_output=True, text=True)
    except FileNotFoundError as exc:
        raise RuntimeError("openssl is required to validate APISIX mTLS secret material") from exc
    except subprocess.CalledProcessError as exc:
        detail = exc.stderr.strip() or "invalid PEM"
        raise RuntimeError(f"{name} failed cryptographic PEM validation: {detail}") from exc
    return content


def yaml_block(value: str, indent: int) -> str:
    prefix = " " * indent
    return "\n".join(f"{prefix}{line}" for line in value.splitlines())


def main() -> None:
    sni = required("DEVICE_GATEWAY_SNI")
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?", sni):
        raise RuntimeError("DEVICE_GATEWAY_SNI must be a lowercase DNS name")
    ssl_id = required("DEVICE_GATEWAY_APISIX_SSL_ID")
    if not ssl_id.isdigit():
        raise RuntimeError("DEVICE_GATEWAY_APISIX_SSL_ID must be numeric")

    cert = pem_from_env_path(
        "DEVICE_GATEWAY_TLS_CERT_FILE", "-----BEGIN CERTIFICATE-----", ["openssl", "x509"]
    )
    key = pem_from_env_path(
        "DEVICE_GATEWAY_TLS_KEY_FILE", "-----BEGIN", ["openssl", "pkey"]
    )
    ca = pem_from_env_path(
        "DEVICE_GATEWAY_CLIENT_CA_FILE", "-----BEGIN CERTIFICATE-----", ["openssl", "x509"]
    )

    base_config = BASE_DIR / "config.yaml"
    base_routes = BASE_DIR / "apisix.yaml"
    if not base_config.is_file() or not base_routes.is_file():
        raise RuntimeError("APISIX base configuration files are missing")

    routes = base_routes.read_text(encoding="utf-8")
    marker = "#END"
    if marker not in routes:
        raise RuntimeError("APISIX standalone configuration is missing its #END marker")
    body, _separator, _tail = routes.rpartition(marker)
    ssl = f"""ssls:
- id: {ssl_id}
  snis:
  - {sni}
  cert: |
{yaml_block(cert, 4)}
  key: |
{yaml_block(key, 4)}
  client:
    ca: |
{yaml_block(ca, 6)}
    depth: 2

#END
"""

    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    shutil.copy2(base_config, OUTPUT_DIR / "config.yaml")
    (OUTPUT_DIR / "apisix.yaml").write_text(body.rstrip() + "\n\n" + ssl, encoding="utf-8")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # fail prior to gateway startup; never emit secret contents
        print(f"device mTLS APISIX configuration rejected: {exc}", file=sys.stderr)
        raise SystemExit(64) from exc
