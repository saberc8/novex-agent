#!/usr/bin/env python3
"""Verify the explicitly assigned global data administrator through real HTTP.

Uses existing synthetic accounts and documents. It does not grant roles, alter
ACLs, upload documents, change business data, or call a model. PostgreSQL is used
only in a read-only transaction to obtain independent resource/document counts.
"""
import hashlib
import json
import urllib.error
import urllib.request
from datetime import datetime, timezone
from decimal import Decimal

import psycopg

from local_config import ROOT, configuration

BASE = 'http://127.0.0.1:8310/api/novex'
TOKENS, USERS, CHECKS = {}, {}, []
REPORT = ROOT / '.local/global-admin-verification.json'


def api(path, actor=None, data=None, expected=200, raw=False):
    headers = {'Content-Type': 'application/json'}
    if actor:
        headers['Authorization'] = 'Bearer ' + TOKENS[actor]
    request = urllib.request.Request(BASE + path, data=json.dumps(data).encode() if data is not None else None, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=40) as response:
            status, body = response.status, response.read()
    except urllib.error.HTTPError as response:
        status, body = response.code, response.read()
    assert status == expected, f'{path}: HTTP {status}, expected {expected}'
    if raw:
        return body
    payload = json.loads(body)
    if status == 200:
        assert payload.get('success'), f'{path}: unsuccessful response'
    return payload.get('data') if status == 200 else payload


def check(name, condition=True):
    assert condition, name
    CHECKS.append(name)
    print('PASS', name, flush=True)


def eq(field, value):
    return {'field': field, 'op': 'eq', 'value': value}


def query(actor, resource, fields=None, filters=None, limit=100):
    return api('/query', actor, {'resourceId': resource, 'fields': fields or [], 'filters': filters or [], 'limit': limit})


def run():
    config = configuration()
    for actor in ('admin', 'a', 'b', 'x', 'scale_admin', 'scale_a', 'scale_c', 'scale_x'):
        TOKENS[actor] = api('/login', data={'username': actor, 'password': config['BOOTSTRAP_ADMIN_PASSWORD']})['token']
        USERS[actor] = api('/me', actor)
    check('Admin explicitly holds novex_super_admin', any(r['code'] == 'novex_super_admin' for r in USERS['admin']['roles']))
    check('Ordinary scale administrator has no global data role', USERS['scale_admin']['isAdmin'] and all(r['code'] != 'novex_super_admin' for r in USERS['scale_admin']['roles']))

    # Do not use an API response as the only oracle for global visibility.
    with psycopg.connect(host=config['DB_HOST'], port=int(config['DB_PORT']), user=config['DB_USER'],
                          password=config['DB_PWD'], dbname=config['DB_NAME'],
                          sslmode=config.get('DB_SSLMODE', 'disable'),
                          options='-c default_transaction_read_only=on') as connection:
        document_ids = {row[0] for row in connection.execute('SELECT id FROM novex_documents')}
        resource_ids = {row[0] for row in connection.execute('SELECT id FROM novex_resources')}
        datasource_count = connection.execute('SELECT count(*) FROM novex_datasources').fetchone()[0]
    resources = api('/admin/resources', 'admin')
    visible = api('/resources', 'admin')
    check('Global resource visibility matches management and independent database oracles',
          {r['id'] for r in visible} == {r['id'] for r in resources} == resource_ids)
    sources = api('/admin/datasources', 'admin')
    expected_databases = {'novex-hr', 'novex-hardware', 'novex-hr-scale', 'novex-hardware-scale'}
    check('All four requested business databases are connected',
          expected_databases.issubset({source['database'] for source in sources}) and len(sources) == datasource_count)
    check('Global account still receives no connection passwords', all('password' not in source and 'passwordEncrypted' not in source for source in sources))
    identity_sources = {item['datasourceId'] for item in api('/admin/identities', 'admin') if item['userId'] == USERS['admin']['id']}
    check('Global access does not depend on per-source identity provisioning', not identity_sources)
    for resource in resources:
        result = query('admin', resource['id'], limit=1)
        check('Global read of registered resource: ' + resource['key'],
              len(result['rows']) == 1 and set(resource['columns']).issubset(result['rows'][0]))

    payroll = query('admin', 'payroll', ['tenant_id', 'employee_id', 'total', 'bank_account'], [eq('employee_id', 'E001'), eq('month', '2026-10')])
    by_tenant = {row['tenant_id']: row for row in payroll['rows']}
    check('Global payroll distinguishes identical employee IDs across acme and globex',
          set(by_tenant) == {'acme', 'globex'} and Decimal(by_tenant['acme']['total']) == 9000 and Decimal(by_tenant['globex']['total']) == 66000)
    check('Explicit global role returns registered sensitive fields without masking', all(row['bank_account'] and row['bank_account'] != '***' for row in payroll['rows']))
    for tenant, stock in [('SC001', 800), ('SC002', 9999)]:
        result = query('admin', 'scale_wms_inventory', ['tenant_id', 'warehouse_id', 'sku', 'stock'],
                       [eq('tenant_id', tenant), eq('warehouse_id', 'W1'), eq('sku', 'HW-000001')])
        check('Global account can explicitly filter external tenant ' + tenant,
              len(result['rows']) == 1 and result['rows'][0]['tenant_id'] == tenant and result['rows'][0]['stock'] == stock)
    products = query('admin', 'scale_wms_products', ['tenant_id', 'sku', 'cost_price'], [eq('tenant_id', 'SC001'), eq('sku', 'HW-000001')])
    check('Global account can read registered procurement cost', len(products['rows']) == 1 and Decimal(products['rows'][0]['cost_price']) == 38)
    api('/query', 'admin', {'resourceId': 'products', 'fields': ['sale_price); DROP TABLE anything; --']}, expected=403)
    check('Global role does not enable arbitrary SQL identifiers')
    bounded = query('admin', 'scale_hr_payroll', limit=1000)
    check('Global queries retain the 100-row limit and partial-result marker', len(bounded['rows']) == 100 and bounded['hasMore'] is True and bounded['rowLimit'] == 100)

    for actor, resource in [('a', 'payroll'), ('scale_a', 'scale_hr_payroll')]:
        result = query(actor, resource, filters=[eq('employee_id', 'E002'), eq('month', '2026-10')])
        check(actor + ' still cannot access another employee salary', result['rows'] == [])
    api('/query', 'scale_a', {'resourceId': 'scale_wms_products', 'fields': ['cost_price']}, expected=403)
    api('/query', 'scale_admin', {'resourceId': 'scale_hr_payroll'}, expected=403)
    check('Ordinary employee cost restriction and ordinary admin no-business-access remain enforced')
    for actor, expected in [('a', 9000), ('x', 66000)]:
        result = query(actor, 'payroll')
        check(actor + ' remains restricted to its own tenant and employee', len(result['rows']) == 1 and Decimal(result['rows'][0]['total']) == expected)

    documents = api('/knowledge', 'admin')
    check('Global knowledge list contains every stored document', {doc['id'] for doc in documents} == document_ids)
    for doc in documents:
        assert api('/knowledge/' + doc['id'], 'admin')['id'] == doc['id'], 'Global document metadata unavailable'
    check('Global metadata reads succeed for every stored Markdown and workbook')
    for document_id in ('demo-a-private', 'demo-hr-private', 'demo-other-tenant'):
        check('Global Markdown read: ' + document_id, bool(api('/knowledge/' + document_id, 'admin')['markdown']))

    fixtures = json.loads((ROOT / '.local/enterprise-fixture-map.json').read_text())['files']
    scale_private = fixtures['hardware-procurement-private.xlsx']
    acme_private = next(doc for doc in documents if doc['ownerId'] == USERS['a']['id'] and doc['fileName'] == 'hardware-procurement-private.xlsx' and doc['scope'] == 'self')
    expected_sha = hashlib.sha256((ROOT / 'fixtures/enterprise/hardware-procurement-private.xlsx').read_bytes()).hexdigest()
    for label, doc in [('acme employee private workbook', acme_private), ('SC001 procurement private workbook', {'id': scale_private['documentId']})]:
        meta = api('/knowledge/' + doc['id'], 'admin')
        result = api('/knowledge/' + doc['id'] + '/search', 'admin', {'sheet': 'Procurement', 'filters': [{'column': 'A', 'op': 'eq', 'value': 'HW-000001'}]})
        original = api('/knowledge/' + doc['id'] + '/file', 'admin', raw=True)
        check('Global metadata, cell search and original download: ' + label,
              meta['scope'] == 'self' and len(result['rows']) == 1
              and Decimal(str(result['rows'][0]['cells']['F'])) == Decimal('42.94')
              and Decimal(str(result['rows'][0]['cells']['G'])) == Decimal('5152.8')
              and hashlib.sha256(original).hexdigest() == expected_sha)
    private_id = scale_private['documentId']
    for actor in ('scale_admin', 'scale_a', 'scale_x'):
        check(actor + ' cannot list another owner private workbook', private_id not in {doc['id'] for doc in api('/knowledge', actor)})
        api('/knowledge/' + private_id, actor, expected=404)
        api('/knowledge/' + private_id + '/search', actor, {'sheet': 'Procurement'}, expected=404)
        api('/knowledge/' + private_id + '/file', actor, expected=404)
    check('Ordinary same-tenant admin, employee and cross-tenant account retain private workbook isolation')

    history = api('/conversations', 'a')
    assert history, 'Expected an existing synthetic employee conversation'
    conversation_id = history[0]['id']
    api('/conversations/' + conversation_id + '/messages', 'admin', expected=404)
    check('Global knowledge/data role does not expose another user conversation history',
          conversation_id not in {item['id'] for item in api('/conversations', 'admin')})
    return {'documents': len(document_ids), 'resources': len(resource_ids), 'datasources': datasource_count,
            'crossTenantPayrollTenants': sorted(by_tenant), 'businessWrites': False, 'modelCalls': False}


def main():
    report = {'at': datetime.now(timezone.utc).isoformat(), 'syntheticFixtures': True, 'checks': CHECKS}
    try:
        report['coverage'] = run()
        report.update({'passed': True, 'passedChecks': len(CHECKS)})
    except Exception as error:
        report.update({'passed': False, 'passedChecks': len(CHECKS), 'failureType': type(error).__name__})
        raise
    finally:
        REPORT.parent.mkdir(exist_ok=True)
        REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps({'report': str(REPORT), 'passedChecks': len(CHECKS), 'coverage': report['coverage']}, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
