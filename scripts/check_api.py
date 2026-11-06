#!/usr/bin/env python3
"""Exercise real local HTTP authentication, data scopes, and iframe tickets.

Uses only the synthetic demo identities. No LLM calls or provider credentials
are needed. Saves a non-sensitive verification summary under .local/.
"""
import json
import sys
import urllib.request
import urllib.error
from decimal import Decimal
from local_config import ROOT, configuration

BASE = 'http://127.0.0.1:8310/api/novex'
checks = []
tokens = {}


def request(path, actor=None, data=None, method=None, expected=200, origin=None):
    headers = {'Content-Type': 'application/json'}
    if actor:
        headers['Authorization'] = 'Bearer ' + tokens[actor]
    if origin:
        headers['Origin'] = origin
    req = urllib.request.Request(BASE + path, data=json.dumps(data).encode() if data is not None else None,
                                 headers=headers, method=method or ('POST' if data is not None else 'GET'))
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            status = response.status
            payload = json.load(response)
    except urllib.error.HTTPError as error:
        status = error.code
        payload = json.load(error)
    assert status == expected, f'{path}: HTTP {status}, expected {expected}: {payload.get("msg", "")}'
    return payload.get('data')


def check(name, condition):
    assert condition, name
    checks.append(name)
    print('PASS', name)


def query(actor, resource, **args):
    return request('/query', actor, {'resourceId': resource, **args})['rows']


def main():
    password = configuration()['BOOTSTRAP_ADMIN_PASSWORD']
    for user in ('admin', 'a', 'b', 'c', 'e', 'x'):
        tokens[user] = request('/login', data={'username': user, 'password': password})['token']
    request('/me', expected=401)
    check('Missing authentication is rejected', True)
    rows = query('a', 'payroll')
    check('A receives only own payroll', len(rows) == 1 and Decimal(rows[0]['total']) == 9000)
    rows = query('a', 'payroll', filters=[{'field': 'employee_id', 'op': 'eq', 'value': 'E004'}])
    check('A cannot retrieve a colleague', rows == [])
    rows = query('b', 'payroll')
    check('B department scope contains three employees', len(rows) == 3)
    check('Multiple roles never grant colleagues bonus details', all(('bonus' in row) == (row['employee_id'] == 'E002') for row in rows))
    request('/query', 'b', {'resourceId': 'payroll', 'filters': [{'field': 'bonus', 'op': 'gte', 'value': 1}]}, expected=403)
    check('Hidden field cannot be used as a filtering oracle', True)
    rows = query('c', 'payroll')
    check('HR can read five employees only in its tenant', len(rows) == 5 and all(Decimal(row['total']) < 66000 for row in rows))
    check('Bank field is masked before response', all(row.get('bank_account') == '***' for row in rows))
    rows = query('x', 'payroll')
    check('Same external employee ID is tenant isolated', len(rows) == 1 and Decimal(rows[0]['total']) == 66000)
    for user, warehouse in [('b', 'W1'), ('e', 'W2')]:
        rows = query(user, 'inventory')
        check(f'{user} warehouse mapping is enforced', bool(rows) and all(row['warehouse_id'] == warehouse for row in rows))
    for payload in [{'resourceId': 'products', 'fields': ['cost_price']},
                    {'resourceId': 'payroll', 'fields': ['total); DROP TABLE anything; --']}]:
        request('/query', 'a', payload, expected=403)
    check('Unauthorized columns and injected identifiers are rejected', True)
    admin_user = request('/me', 'admin')
    if any(role['code'] == 'novex_super_admin' for role in admin_user['roles']):
        rows = query('admin', 'payroll')
        check('Explicit global data role reads both synthetic payroll tenants',
              len(rows) == 6 and {row['tenant_id'] for row in rows} == {'acme', 'globex'})
    else:
        request('/query', 'admin', {'resourceId': 'payroll'}, expected=403)
        check('Platform admin without the explicit global data role has no payroll bypass', True)
    request('/admin/datasources', 'a', expected=403)
    check('Employees have no connection management', True)
    sources = request('/admin/datasources', 'admin')
    check('Connection secrets never return to UI', len(sources) >= 2 and all('password' not in source and 'passwordEncrypted' not in source for source in sources))
    for source in sources[:2]:
        result = request('/admin/datasources/test', 'admin', {'id': source['id']})
        check(f'External connection test: {source["database"]}', result['ok'] and result['readOnly'])
    # A ticket is audience-bound, single-use, and exchanged by the chat origin.
    ticket = request('/embed-tickets', 'a', {'origin': 'http://localhost:8311'})['ticket']
    request('/embed-exchange', data={'ticket': ticket, 'origin': 'http://localhost:8311'}, origin='https://untrusted.example', expected=403)
    embedded = request('/embed-exchange', data={'ticket': ticket, 'origin': 'http://localhost:8311'}, origin='http://localhost:8312')
    check('Iframe ticket retains the issuing user', embedded['user']['username'] == 'a')
    request('/embed-exchange', data={'ticket': ticket, 'origin': 'http://localhost:8311'}, origin='http://localhost:8312', expected=401)
    check('Iframe launch ticket cannot be replayed', True)
    conv = request('/conversations', 'a', {'title': 'HTTP 隔离验收', 'runtime': 'pi'})
    request('/conversations/' + conv['id'] + '/messages', 'b', expected=404)
    check('Conversation IDs do not authorize cross-user history', True)
    knowledge = request('/knowledge', 'a')
    check('Knowledge listing is filtered for ordinary employees', all('HR' not in item['title'] for item in knowledge))
    request('/logout', 'a', {})
    request('/me', 'a', expected=401)
    check('Logout revokes the authenticated session', True)
    (ROOT / '.local/api-verification.json').write_text(json.dumps({'passed': len(checks), 'checks': checks}, ensure_ascii=False, indent=2))
    print(f'HTTP verification complete: {len(checks)} checks passed.')


if __name__ == '__main__':
    main()
