#!/usr/bin/env python3
"""Real HTTP XLSX/CSV verification; no LLM calls.

Default: functional checks only. Run --mode benchmark after the scale-data
bootstrap has finished, or --mode all when the local database is otherwise idle.
Benchmark concurrency 1/4/8 each runs for at least 15 seconds and 60 requests.
Two clearly titled acme demo workbooks are retained for UI testing. Temporary
CSV, malformed-format and formula-cache fixtures are removed in a finally block.
No SC001 workbooks or accounts are modified.
"""
import argparse
import concurrent.futures
import hashlib
import io
import json
import math
import time
import threading
import urllib.error
import urllib.request
import uuid
import xml.etree.ElementTree as ET
import zipfile
from datetime import datetime, timezone
from decimal import Decimal

from local_config import ROOT, configuration

BASE = 'http://127.0.0.1:8310/api/novex'
FIXTURES = ROOT / 'fixtures/enterprise'
REPORT = ROOT / '.local/spreadsheet-verification.json'
TOKENS = {}
USERS = {}
CHECKS = []
TEMP_DOCS = []
TITLES = {
    'catalog': '验收 · acme 五金历史报价目录 50000 行',
    'procurement': '验收 · a 私有采购报价与供应商条款',
}


def http(path, actor=None, data=None, method=None, expected=200, raw=False, timeout=40, headers=None):
    request_headers = dict(headers or {})
    if actor:
        request_headers['Authorization'] = 'Bearer ' + TOKENS[actor]
    if data is not None and not isinstance(data, bytes):
        data = json.dumps(data, ensure_ascii=False).encode()
        request_headers['Content-Type'] = 'application/json'
    request = urllib.request.Request(BASE + path, data=data, method=method or ('POST' if data is not None else 'GET'), headers=request_headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            status, content, returned_headers = response.status, response.read(), dict(response.headers)
    except urllib.error.HTTPError as response:
        status, content, returned_headers = response.code, response.read(), dict(response.headers)
    if status != expected:
        try:
            reason = json.loads(content).get('msg', '')
        except (ValueError, UnicodeDecodeError):
            reason = 'non-JSON response'
        raise AssertionError(f'{path}: HTTP {status}, expected {expected}: {reason}')
    if raw:
        return content, returned_headers
    payload = json.loads(content)
    if expected == 200:
        assert payload.get('success'), f'{path}: unsuccessful response'
    return payload.get('data') if expected == 200 else payload


def check(name, condition=True):
    assert condition, name
    CHECKS.append(name)
    print('PASS', name, flush=True)


def login():
    password = configuration()['BOOTSTRAP_ADMIN_PASSWORD']
    for actor in ('admin', 'a', 'b', 'x'):
        TOKENS[actor] = http('/login', data={'username': actor, 'password': password})['token']
        USERS[actor] = http('/me', actor)
    for actor in ('admin', 'a', 'b'):
        assert http('/me', actor)['tenantId'] == 'acme', 'Refusing to run outside synthetic acme tenant'
    assert http('/me', 'x')['tenantId'] == 'globex'
    # The scale suite is optional; when installed it supplies an ordinary
    # platform administrator without the explicit global data role.
    if any(user['username'] == 'scale_admin' for user in http('/admin/users', 'admin')):
        TOKENS['scale_admin'] = http('/login', data={'username': 'scale_admin', 'password': password})['token']
        USERS['scale_admin'] = http('/me', 'scale_admin')
        assert not any(role['code'] == 'novex_super_admin' for role in USERS['scale_admin']['roles']), 'scale_admin must remain an ordinary administrator for this isolation check'


def upload(actor, filename, content, title, scope='self', expected=200):
    boundary = 'NovexSpreadsheet' + uuid.uuid4().hex
    chunks = []
    for key, value in [('title', title), ('scope', scope)]:
        chunks.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
    chunks.append(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode())
    chunks.extend([content, f'\r\n--{boundary}--\r\n'.encode()])
    return http('/knowledge', actor, b''.join(chunks), expected=expected, timeout=240, headers={'Content-Type': 'multipart/form-data; boundary=' + boundary})


def ensure_fixture(key, actor, path, scope, expected_sha):
    local = path.read_bytes()
    assert hashlib.sha256(local).hexdigest() == expected_sha, 'Fixture changed since manifest was generated'
    matching = [doc for doc in http('/knowledge', actor) if doc['title'] == TITLES[key] and doc['fileName'] == path.name]
    for doc in matching:
        original, _ = http('/knowledge/' + doc['id'] + '/file', actor, raw=True)
        if hashlib.sha256(original).hexdigest() == expected_sha:
            print('REUSE', key, doc['id'], flush=True)
            return doc
    assert not matching, 'An existing verification workbook differs: refusing to replace it automatically'
    started = time.perf_counter()
    doc = upload(actor, path.name, local, TITLES[key], scope)
    print('IMPORTED', key, f'{time.perf_counter() - started:.2f}s', doc['id'], flush=True)
    return doc


def search(doc, actor, **body):
    return http('/knowledge/' + doc['id'] + '/search', actor, body)


def exact_catalog(doc, actor='a', keyword=False, numeric=False):
    filters = [{'column': 'B', 'op': 'eq', 'value': '2026-11'}]
    if not keyword:
        filters.append({'column': 'A', 'op': 'eq', 'value': 'HW-002499'})
    if numeric:
        filters += [{'column': 'E', 'op': 'gte', 'value': 888.25}, {'column': 'E', 'op': 'lte', 'value': 888.25}]
    result = search(doc, actor, sheet='Catalog', query='HW-002499' if keyword else '', filters=filters, limit=20)
    assert len(result['rows']) == 1, 'catalog query must return one exact row'
    row = result['rows'][0]
    assert row['row'] == 50000 and row['cells']['A'] == 'HW-002499' and row['cells']['B'] == '2026-11'
    assert Decimal(str(row['cells']['E'])) == Decimal('888.25'), 'catalog price differs from oracle'
    assert row['citation'] == f'[document:{doc["id"]}] Catalog!A50000:I50000'
    return result


def patched_formula(content):
    source = zipfile.ZipFile(io.BytesIO(content))
    output = io.BytesIO()
    namespace = '{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    changed = False
    with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as target:
        for item in source.infolist():
            data = source.read(item.filename)
            if item.filename == 'xl/worksheets/sheet1.xml':
                tree = ET.fromstring(data)
                for cell in tree.iter(namespace + 'c'):
                    if cell.get('r') == 'G2' and cell.find(namespace + 'f') is not None:
                        cached = cell.find(namespace + 'v')
                        if cached is not None:
                            cell.remove(cached)
                        changed = True
                data = ET.tostring(tree, encoding='utf-8', xml_declaration=True)
            target.writestr(item, data)
    source.close()
    assert changed, 'Expected Procurement!G2 formula in fixture'
    return output.getvalue()



def stale_dimensions(content):
    output = io.BytesIO()
    namespace = '{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    with zipfile.ZipFile(io.BytesIO(content)) as source, zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as target:
        for item in source.infolist():
            data = source.read(item.filename)
            if item.filename.startswith('xl/worksheets/sheet') and item.filename.endswith('.xml'):
                tree = ET.fromstring(data)
                dimension = tree.find(namespace + 'dimension')
                if dimension is None:
                    dimension = ET.Element(namespace + 'dimension')
                    tree.insert(0, dimension)
                dimension.set('ref', 'A1')
                data = ET.tostring(tree, encoding='utf-8', xml_declaration=True)
            target.writestr(item, data)
    return output.getvalue()


def temporary(filename, data, title):
    doc = upload('a', filename, data, '临时表格验收 · ' + title)
    TEMP_DOCS.append(doc['id'])
    return doc


def functional(catalog, procurement, manifest):
    details = {}
    for key, doc, actor in [('catalog', catalog, 'admin'), ('procurement', procurement, 'a')]:
        meta = http('/knowledge/' + doc['id'], actor)
        oracle = manifest['workbooks'][0 if key == 'catalog' else 1]
        by_name = {sheet['name']: sheet for sheet in meta['sheets']}
        check(f'{key}: structured metadata retains all worksheets', meta['kind'] == 'spreadsheet' and not meta.get('markdown') and len(by_name) == 2)
        check(f'{key}: row and column counts match workbook', meta['rowCount'] == sum(sheet['rows'] for sheet in oracle['sheets'].values()) and all(by_name[name]['rowCount'] == item['rows'] and by_name[name]['columnCount'] == item['columns'] for name, item in oracle['sheets'].items()))
        original, headers = http('/knowledge/' + doc['id'] + '/file', actor, raw=True)
        check(f'{key}: authenticated download is byte-for-byte original', hashlib.sha256(original).hexdigest() == oracle['sha256'] and headers.get('Cache-Control') == 'private, no-store')
        details[key] = meta
    catalog_columns = {column['key']: column for column in next(sheet for sheet in details['catalog']['sheets'] if sheet['name'] == 'Catalog')['columns']}
    check('XLSX strings and numbers retain explicit column types', catalog_columns['A']['type'] == 'string' and catalog_columns['E']['type'] == 'number')
    exact_catalog(catalog)
    check('Exact SKU + month returns oracle price, Excel row and cell citation')
    exact_catalog(catalog, keyword=True)
    check('Keyword retrieval returns the same exact row')
    exact_catalog(catalog, numeric=True)
    check('Numeric lower and upper bounds preserve the oracle value')
    previous = search(catalog, 'a', sheet='Catalog', filters=[{'column': 'A', 'op': 'eq', 'value': 'HW-002499'}, {'column': 'B', 'op': 'eq', 'value': '2026-10'}])['rows']
    check('Historical month is not silently replaced by latest price', len(previous) == 1 and previous[0]['row'] == 47500 and Decimal(str(previous[0]['cells']['E'])) == Decimal('888.15'))
    filters = [{'column': 'A', 'op': 'eq', 'value': 'HW-002499'}]
    first = search(catalog, 'a', sheet='Catalog', filters=filters, limit=5, offset=0)
    last = search(catalog, 'a', sheet='Catalog', filters=filters, limit=5, offset=15)
    beyond = search(catalog, 'a', sheet='Catalog', filters=filters, limit=5, offset=20)
    check('Pagination reports hasMore accurately including exact last page', len(first['rows']) == 5 and first['hasMore'] is True and len(last['rows']) == 5 and last['hasMore'] is False and not beyond['rows'] and beyond['hasMore'] is False)
    tail = search(catalog, 'a', sheet='Catalog', limit=20, offset=49999)
    check('Large workbook pagination reaches rows beyond the old 10000 offset cap', len(tail['rows']) == 1 and tail['rows'][0]['row'] == 50001 and tail['hasMore'] is False)
    row = search(procurement, 'a', sheet='Procurement', filters=[{'column': 'A', 'op': 'eq', 'value': 'HW-000001'}])['rows'][0]
    check('Cached Excel formula results remain numeric and are not re-executed', row['row'] == 2 and Decimal(str(row['cells']['F'])) == Decimal('42.94') and Decimal(str(row['cells']['G'])) == Decimal('5152.8') and row.get('formulas', {}).get('G', '').startswith('=ROUND'))
    no_cache = temporary('formula-no-cache.xlsx', patched_formula((FIXTURES / 'hardware-procurement-private.xlsx').read_bytes()), '无缓存公式')
    no_cache_meta = http('/knowledge/' + no_cache['id'], 'a')
    no_cache_row = search(no_cache, 'a', sheet='Procurement', filters=[{'column': 'A', 'op': 'eq', 'value': 'HW-000001'}])['rows'][0]
    check('Missing formula cache emits a cell-specific warning instead of inventing zero', any('G2' in warning for warning in no_cache_meta.get('warnings', [])) and no_cache_row['cells'].get('G') is None)
    stale = temporary('stale-dimensions.xlsx', stale_dimensions((FIXTURES / 'hardware-procurement-private.xlsx').read_bytes()), '过期Excel区域缓存')
    stale_meta = http('/knowledge/' + stale['id'], 'a')
    check('Stale XLSX dimension metadata cannot silently truncate actual cells', stale_meta['rowCount'] == 28 and {sheet['name']: sheet['rowCount'] for sheet in stale_meta['sheets']} == {'Procurement': 24, 'SupplierTerms': 4})
    csv_bytes = ('\ufeffSKU,units,notes\r\n0012,12,"literal 100%_match"\r\n0013,20,中文产品\r\n0014,25,other\r\n').encode('utf-8')
    csv_doc = temporary('verification.csv', csv_bytes, 'UTF8 CSV')
    csv_meta = http('/knowledge/' + csv_doc['id'], 'a')
    csv_result = search(csv_doc, 'a', sheet='Sheet1', filters=[{'column': 'A', 'op': 'eq', 'value': '0012'}])
    check('CSV preserves UTF-8, leading-zero identifiers and source line numbers', csv_meta['rowCount'] == 3 and csv_result['rows'][0]['row'] == 2 and csv_result['rows'][0]['cells']['A'] == '0012' and csv_result['rows'][0]['cells']['B'] == '12')
    literal = search(csv_doc, 'a', sheet='Sheet1', query='100%_match')
    check('LIKE wildcard characters are searched literally', len(literal['rows']) == 1 and literal['rows'][0]['cells']['A'] == '0012')
    csv_original, _ = http('/knowledge/' + csv_doc['id'] + '/file', 'a', raw=True)
    check('CSV download retains original bytes including BOM and CRLF', csv_original == csv_bytes)
    blank_csv = temporary('leading-empty-rows.csv', '\ufeff,,\r\n\r\nSKU,units,notes\r\n0012,12,entry\r\n,,\r\n'.encode(), 'CSV首个非空行')
    blank_meta = http('/knowledge/' + blank_csv['id'], 'a')
    blank_result = search(blank_csv, 'a', sheet='Sheet1')
    check('CSV skips entirely blank cells while preserving original header and data line numbers', blank_meta['sheets'][0]['headerRow'] == 3 and blank_meta['rowCount'] == 1 and blank_result['rows'][0]['row'] == 4 and blank_result['rows'][0]['cells']['A'] == '0012')
    for invalid in [
        {'sheet': 'Catalog', 'filters': [{'column': "A'); DROP TABLE anything;--", 'op': 'eq', 'value': 'x'}]},
        {'sheet': 'Catalog', 'filters': [{'column': 'A', 'op': 'delete', 'value': 'x'}]},
        {'sheet': 'Catalog', 'query': 'x' * 201},
        {'sheet': 'Catalog', 'filters': [{'column': 'A', 'op': 'gte', 'value': 1}]},
        {'sheet': 'not-a-sheet'},
        {'filters': [{'column': 'A', 'op': 'eq', 'value': 'x'}]},
        {'sheet': 'Catalog', 'offset': -1},
        {'sheet': 'Catalog', 'offset': 200001},
        {'sheet': 'Catalog', 'filters': [{'column': 'A', 'op': 'eq', 'value': {'unexpected': 'object'}}]},
        {'sheet': 'Catalog', 'filters': [{'column': 'A', 'op': 'contains', 'value': 'x' * 201}]},
        {'sheet': 'Catalog', 'filters': [{'column': 'E', 'op': 'gte', 'value': 'NaN'}]},
        {'sheet': 'Catalog', 'filters': [{'column': 'A', 'op': 'eq', 'value': 'x'}] * 9},
    ]:
        http('/knowledge/' + catalog['id'] + '/search', 'a', invalid, expected=400)
    check('Invalid column/operator/sheet/types, nested filters, long filter values and pagination bounds are rejected')
    http('/knowledge/' + catalog['id'] + '/search', 'a', {'sheet': 'Catalog', 'query': 'x' * 70000}, expected=400)
    check('Spreadsheet query request bodies are capped at 64 KiB')
    http('/knowledge/' + csv_doc['id'] + '/search', 'a', {'sheet': 'Sheet1', 'filters': [{'column': 'B', 'op': 'gte', 'value': 10}]}, expected=400)
    check('CSV text columns cannot be silently coerced by numeric comparisons')
    for filename, contents in [('forged.xlsx', b'SKU,price\nP1,10\n'), ('legacy.xls', b'not an xls'), ('binary.csv', b'A,B\n\xff,2\n')]:
        upload('a', filename, contents, '临时格式拒绝测试', expected=400)
    check('Disguised XLSX, unsupported XLS and non-UTF8 CSV are rejected')
    denied = [('b', procurement), ('x', catalog), ('x', procurement)]
    if any(role['code'] == 'novex_super_admin' for role in USERS['admin']['roles']):
        meta = http('/knowledge/' + procurement['id'], 'admin')
        result = search(procurement, 'admin', sheet='Procurement', filters=[{'column': 'A', 'op': 'eq', 'value': 'HW-000001'}])
        original, _ = http('/knowledge/' + procurement['id'] + '/file', 'admin', raw=True)
        check('Explicit global data role can read and download another owner\'s private workbook',
              meta['id'] == procurement['id'] and len(result['rows']) == 1
              and hashlib.sha256(original).hexdigest() == manifest['workbooks'][1]['sha256'])
    else:
        denied.append(('admin', procurement))
    if 'scale_admin' in TOKENS:
        denied.append(('scale_admin', procurement))
    for actor, forbidden in denied:
        listed = http('/knowledge', actor)
        check(f'{actor}: inaccessible document absent from listing ({forbidden["id"][:8]})', all(doc['id'] != forbidden['id'] for doc in listed))
        http('/knowledge/' + forbidden['id'], actor, expected=404)
        http('/knowledge/' + forbidden['id'] + '/search', actor, {'query': 'HW-000001'}, expected=404)
        http('/knowledge/' + forbidden['id'] + '/file', actor, expected=404)
    check('Ordinary users and administrators without global data access remain subject to document ACLs (scale_admin also crosses tenants)')
    missing = http('/knowledge/' + '0' * 32, 'b', expected=404)
    hidden = http('/knowledge/' + procurement['id'], 'b', expected=404)
    check('Hidden document IDs cannot be distinguished from unknown document IDs', missing.get('msg') == hidden.get('msg'))
    http('/knowledge/' + catalog['id'] + '/file', expected=401)
    check('Original file downloads require authentication')
    return {'passed': len(CHECKS), 'checks': CHECKS}


def percentile(values, fraction):
    ordered = sorted(values)
    return round(ordered[max(0, math.ceil(len(ordered) * fraction) - 1)], 3) if ordered else None


def benchmark(catalog, duration_seconds=15):
    results = []
    for i in range(3):
        exact_catalog(catalog, keyword=i == 1, numeric=i == 2)
    for concurrency in (1, 4, 8):
        gate = threading.Event()
        lock = threading.Lock()
        allocated = 0
        started = 0.0

        def worker():
            nonlocal allocated
            local_timings = []
            gate.wait()
            while True:
                with lock:
                    if allocated >= 60 and time.perf_counter() - started >= duration_seconds:
                        break
                    index = allocated
                    allocated += 1
                request_started = time.perf_counter()
                try:
                    exact_catalog(catalog, keyword=index % 3 == 1, numeric=index % 3 == 2)
                    local_timings.append(((time.perf_counter() - request_started) * 1000, None))
                except Exception as error:
                    local_timings.append(((time.perf_counter() - request_started) * 1000, str(error)[:300]))
            return local_timings

        with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
            workers = [pool.submit(worker) for _ in range(concurrency)]
            started = time.perf_counter()
            gate.set()
            timings = [timing for future in workers for timing in future.result()]
        elapsed = time.perf_counter() - started
        latency = [ms for ms, error in timings if error is None]
        errors = [error for _, error in timings if error is not None]
        count = len(timings)
        item = {
            'concurrency': concurrency, 'requests': count, 'minimumRequests': 60,
            'targetDurationSeconds': duration_seconds,
            'workload': 'Round-robin exact, keyword and numeric-bound SKU/month queries; every successful result asserted against row 50000 and price 888.25',
            'p50Ms': percentile(latency, .50), 'p95Ms': percentile(latency, .95),
            'p99Ms': percentile(latency, .99), 'errors': len(errors),
            'errorDetails': errors, 'elapsedSeconds': round(elapsed, 3),
            'requestsPerSecond': round(count / elapsed, 2),
            'successfulRequestsPerSecond': round(len(latency) / elapsed, 2),
            'latencyMs': [round(ms, 3) for ms, _ in timings],
        }
        results.append(item)
        print('BENCHMARK', json.dumps({key: value for key, value in item.items() if key not in ('latencyMs', 'errorDetails', 'workload')}, ensure_ascii=False), flush=True)
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('functional', 'benchmark', 'all'), default='functional')
    parser.add_argument('--duration-seconds', type=float, default=15, help='Minimum steady-state seconds per concurrency (at least 15; also at least 60 requests)')
    args = parser.parse_args()
    if not math.isfinite(args.duration_seconds) or args.duration_seconds < 15:
        parser.error('--duration-seconds must be at least 15')
    login()
    manifest = json.loads((FIXTURES / 'manifest.json').read_text())
    catalog = ensure_fixture('catalog', 'admin', FIXTURES / manifest['workbooks'][0]['path'], 'tenant', manifest['workbooks'][0]['sha256'])
    procurement = ensure_fixture('procurement', 'a', FIXTURES / manifest['workbooks'][1]['path'], 'self', manifest['workbooks'][1]['sha256'])
    report = json.loads(REPORT.read_text()) if REPORT.exists() else {}
    report.update({'at': datetime.now(timezone.utc).isoformat(), 'synthetic': True, 'base': BASE, 'retainedDocuments': {'catalog': {'id': catalog['id'], 'title': TITLES['catalog'], 'owner': 'admin', 'scope': 'tenant', 'tenant': 'acme', 'sha256': manifest['workbooks'][0]['sha256']}, 'procurement': {'id': procurement['id'], 'title': TITLES['procurement'], 'owner': 'a', 'scope': 'self', 'tenant': 'acme', 'sha256': manifest['workbooks'][1]['sha256']}}})
    failure = None
    try:
        if args.mode in ('functional', 'all'):
            report['functional'] = functional(catalog, procurement, manifest)
        if args.mode in ('benchmark', 'all'):
            report['benchmark'] = benchmark(catalog, args.duration_seconds)
            assert all(item['errors'] == 0 for item in report['benchmark']), 'Spreadsheet benchmark had errors'
        report['passed'] = True
    except Exception as error:
        failure = error
        report.update({'passed': False, 'failure': str(error), 'completedChecks': CHECKS})
    finally:
        cleanup_errors = []
        for document_id in TEMP_DOCS:
            try:
                http('/knowledge/' + document_id, 'a', method='DELETE')
            except Exception as error:
                cleanup_errors.append(str(error))
        report['temporaryCleanupErrors'] = cleanup_errors
        REPORT.parent.mkdir(exist_ok=True)
        REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2))
    if failure:
        raise failure
    assert not report['temporaryCleanupErrors'], 'Temporary fixture cleanup failed'
    print('Spreadsheet verification saved to', REPORT, flush=True)


if __name__ == '__main__':
    main()
