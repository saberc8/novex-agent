#!/usr/bin/env python3
"""Generate ignored local demo configuration; preserve it on subsequent runs."""
import base64
import getpass
import os
import secrets
import subprocess
from pathlib import Path
from local_config import ROOT


def main():
    output = ROOT / '.env.local'
    if output.exists():
        print('Existing .env.local preserved. Edit it privately to change configuration.')
        return
    api_key = os.environ.get('NOVEX_MODEL_API_KEY') or getpass.getpass('api.b.ai API key (stored only in ignored .env.local): ')
    if not api_key.strip():
        raise SystemExit('A model key is required for real agent tests.')
    demo_password = os.environ.get('BOOTSTRAP_ADMIN_PASSWORD') or getpass.getpass('Demo account password (at least 12 characters): ')
    if len(demo_password) < 12:
        raise SystemExit('Use a password of at least 12 characters.')
    pem = subprocess.run(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:2048'], capture_output=True, check=True).stdout
    der = subprocess.run(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-outform', 'DER'], input=pem, capture_output=True, check=True).stdout
    public = subprocess.run(['openssl', 'pkey', '-pubout'], input=pem, capture_output=True, check=True).stdout.decode()
    key = base64.b64encode(secrets.token_bytes(32)).decode()
    values = {
        'APP_ENV': 'development', 'GIN_MODE': 'release', 'HTTP_PORT': '8310',
        'DB_HOST': os.environ.get('PGHOST', '127.0.0.1'), 'DB_PORT': os.environ.get('PGPORT', '5432'),
        'DB_USER': 'novex_app', 'DB_PWD': secrets.token_hex(20), 'DB_NAME': 'novex-agent', 'DB_SSLMODE': 'disable',
        'REDIS_HOST': '127.0.0.1', 'REDIS_PORT': '18379', 'REDIS_DB': '0',
        'AUTH_JWT_SECRET': secrets.token_hex(32), 'AUTH_RSA_PRIVATE_KEY': base64.b64encode(der).decode(),
        'BOOTSTRAP_ADMIN_PASSWORD': demo_password,
        'HTTP_CORS_ALLOWED_ORIGINS': 'http://localhost:8311,http://127.0.0.1:8311,http://localhost:8312,http://127.0.0.1:8312',
        'NOVEX_ENCRYPTION_KEY': key, 'NOVEX_RUNNER_SECRET': secrets.token_hex(32),
        'NOVEX_RUNNER_URL': 'http://127.0.0.1:8320', 'NOVEX_MODEL_PROXY_URL': 'http://127.0.0.1:8330',
        'NOVEX_MODEL_BASE_URL': 'https://api.b.ai/v1', 'NOVEX_MODEL_NAME': 'deepseek-v4.1-flash',
        'NOVEX_MODEL_API_KEY': api_key.strip(), 'NOVEX_HTTPS_PROXY': os.environ.get('HTTPS_PROXY', ''),
        'NOVEX_HR_HOST': os.environ.get('PGHOST', '127.0.0.1'), 'NOVEX_HR_PORT': os.environ.get('PGPORT', '5432'),
        'NOVEX_HR_DATABASE': 'novex-hr', 'NOVEX_HR_USER': 'novex_hr_read', 'NOVEX_HR_PASSWORD': secrets.token_hex(20),
        'NOVEX_HARDWARE_HOST': os.environ.get('PGHOST', '127.0.0.1'), 'NOVEX_HARDWARE_PORT': os.environ.get('PGPORT', '5432'),
        'NOVEX_HARDWARE_DATABASE': 'novex-hardware', 'NOVEX_HARDWARE_USER': 'novex_hardware_read', 'NOVEX_HARDWARE_PASSWORD': secrets.token_hex(20),
        'NOVEX_DATA_DIR': str(ROOT / '.local/data'), 'NOVEX_PUBLIC_URL': 'http://host.docker.internal:8310',
        'NOVEX_PYTHON': str(ROOT / '.local/python/bin/python'),
        'NOVEX_EXECUTION_MODE': 'docker', 'NOVEX_WORKSPACE_ROOT': str(ROOT / '.local/data/workspaces'),
        'NOVEX_MCP_ALLOWED_ORIGIN': 'http://host.docker.internal:8310', 'NOVEX_RUN_TIMEOUT_MS': '240000',
        'NOVEX_CHAT_ORIGINS': 'http://localhost:8312,http://127.0.0.1:8312',
        'NOVEX_EMBED_PARENT_ORIGINS': 'http://localhost:8311,http://127.0.0.1:8311',
    }
    if any('\n' in value or '\r' in value for value in values.values()):
        raise SystemExit('Configuration values must be single-line.')
    output.write_text(''.join(f'{name}={value}\n' for name, value in values.items()))
    output.chmod(0o600)
    private = ROOT / '.local'
    private.mkdir(mode=0o700, exist_ok=True)
    (private / 'data/workspaces').mkdir(mode=0o700, parents=True, exist_ok=True)
    (private / 'public.pem').write_text(public)
    admin_env = ROOT / 'admin/.env.development.local'
    admin_env.write_text('VITE_API_BASE_URL=http://localhost:8310\nVITE_API_PREFIX=/dev-api\nVITE_PORT=8311\nVITE_RSA_PUBLIC_KEY="' + public.replace('\n', '\\n') + '"\n')
    print('Generated private local configuration and admin public-key settings.')


if __name__ == '__main__':
    main()
