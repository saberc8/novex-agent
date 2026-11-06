#!/usr/bin/env python3
"""Verify the original RBAC account flow reaches Chat through explicit provisioning.

Creates one synthetic user through /system/user and cleans it up afterwards.
Uses local public-key encryption and never prints credentials. Changes the
policy version, so run before interactive demonstrations.
"""
import argparse
import base64
import json
import os
import secrets
import subprocess
import urllib.error
import urllib.request
from local_config import ROOT, configuration

BASE = 'http://127.0.0.1:8310'


def request(path, token='', data=None, method=None, expected=200):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(BASE + path, headers=headers,
                                 data=json.dumps(data).encode() if data is not None else None,
                                 method=method or ('POST' if data is not None else 'GET'))
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            status, payload = response.status, json.load(response)
    except urllib.error.HTTPError as error:
        status, payload = error.code, json.load(error)
    assert status == expected, f'{path}: HTTP {status}, expected {expected}: {payload.get("msg", "")}'
    if expected == 200:
        assert payload['success'], f'{path}: {payload.get("msg", "")}'
    return payload.get('data')


def paths(items):
    found = set()
    for item in items:
        found.add(item.get('path', ''))
        found.update(paths(item.get('children') or []))
    return found


def check_menus(admin, employee):
    routes = request('/auth/user/route', admin)
    novex = next(item for item in routes if item['path'] == '/novex')
    pages = novex.get('children') or []
    expected = {
        '/novex/console': 'novex/overview/index',
        **{f'/novex/{page}': f'novex/{page}/index' for page in (
            'datasources', 'resources', 'identities', 'grants', 'preview', 'knowledge', 'audit', 'chat')},
    }
    assert {item['path']: item['component'] for item in pages} == expected, 'Novex sidebar pages do not match independent route components'
    assert len({item['name'] for item in pages}) == 9, 'Independent pages need distinct route names'
    assert not (set(expected) & paths(request('/auth/user/route', employee))), 'Employee received Novex management menus'
    request('/api/novex/admin/users', employee, expected=403)
    print('PASS Nine independent Novex sidebar routes; employees have no management menus or APIs')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--menus-only', action='store_true', help='Read-only menu/authorization checks; do not create a test account')
    args = parser.parse_args()
    conf = configuration()
    admin = request('/api/novex/login', data={
        'username': 'admin', 'password': conf['BOOTSTRAP_ADMIN_PASSWORD']})['token']
    employee = request('/api/novex/login', data={
        'username': 'a', 'password': conf['BOOTSTRAP_ADMIN_PASSWORD']})['token']
    check_menus(admin, employee)
    if args.menus_only:
        return

    username = 'rbac_check_' + secrets.token_hex(5)
    password = 'RbacCheck2026!' + secrets.token_hex(5)
    encrypted = subprocess.run([
        'openssl', 'pkeyutl', '-encrypt', '-pubin', '-inkey', str(ROOT / '.local/public.pem'),
        '-pkeyopt', 'rsa_padding_mode:pkcs1',
    ], input=password.encode(), capture_output=True, check=True).stdout
    uid = request('/system/user', admin, {
        'username': username, 'nickname': 'RBAC integration check',
        'password': base64.b64encode(encrypted).decode(), 'status': 1,
        'deptId': 1, 'roleIds': [9101],
    })['id']
    assert isinstance(uid, int) and uid > 0
    try:
        user = next(u for u in request('/api/novex/admin/users', admin) if u['id'] == uid)
        assert not user['novexEnabled'] and user['systemDeptId'] == 1
        request('/api/novex/login', data={'username': username, 'password': password}, expected=401)
        print('PASS Account created in original RBAC is visible but cannot enter Chat before provisioning')

        request(f'/api/novex/admin/users/{uid}/profile', employee,
                {'tenantId': 'acme', 'departmentId': 'sales'}, method='PUT', expected=403)
        request(f'/api/novex/admin/users/{uid}/profile', admin,
                {'tenantId': 'acme', 'departmentId': 'sales'}, method='PUT')
        login = request('/api/novex/login', data={'username': username, 'password': password})
        assert login['user']['id'] == uid
        request('/api/novex/query', login['token'], {'resourceId': 'payroll'}, expected=403)
        print('PASS Explicit provisioning reuses the same credentials; missing external identity still denies access')

        hr = next(d for d in request('/api/novex/admin/datasources', admin) if d['database'] == 'novex-hr')
        request('/api/novex/admin/identities', admin, {
            'userId': uid, 'datasourceId': hr['id'], 'tenantId': 'acme',
            'externalTenantId': 'acme', 'employeeId': 'E001', 'departmentId': 'sales', 'warehouseIds': [],
        })
        rows = request('/api/novex/query', login['token'], {'resourceId': 'payroll'})['rows']
        assert len(rows) == 1 and rows[0]['employee_id'] == 'E001'
        request(f'/api/novex/admin/users/{uid}/profile', admin,
                {'tenantId': 'other-company', 'departmentId': 'sales'}, method='PUT', expected=409)
        print('PASS Shared role grants apply after mapping; existing mappings prevent tenant reinterpretation')
        request('/api/novex/logout', login['token'], {})
    finally:
        # Only remove records attached to the synthetic user created above.
        env = dict(os.environ)
        env.update({'PGHOST': conf['DB_HOST'], 'PGPORT': conf['DB_PORT'], 'PGUSER': conf['DB_USER'],
                    'PGPASSWORD': conf['DB_PWD'], 'PGDATABASE': conf['DB_NAME']})
        result = subprocess.run(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At'], env=env,
                                input=f'BEGIN; DELETE FROM novex_identities WHERE user_id={uid}; '
                                      f'DELETE FROM novex_users WHERE user_id={uid}; COMMIT;',
                                text=True, capture_output=True)
        if result.returncode:
            raise RuntimeError('Synthetic test profile cleanup failed; no credentials were printed.')
        request('/system/user', admin, {'ids': [uid]}, method='DELETE')
        print('PASS Synthetic test account cleaned up through original RBAC')


if __name__ == '__main__':
    main()
