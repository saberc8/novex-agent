#!/usr/bin/env python3
"""Paid, real end-to-end agent checks against the seeded demo databases."""
import concurrent.futures
import json
import time
from local_config import ROOT, configuration
from check_api import request, tokens


def run(user, runtime, prompt):
    conversation = request('/conversations', user, {'title': f'{runtime} 实际业务验收', 'runtime': runtime})
    started = request(f'/conversations/{conversation["id"]}/messages', user, {'content': prompt})
    run_id = started['runId']
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        result = request('/runs/' + run_id, user)
        if result['status'] in ('completed', 'failed', 'error', 'cancelled'):
            if result['status'] != 'completed':
                raise AssertionError(f'{runtime}: {result["status"]}: {result.get("error")}')
            print(f'{runtime}: completed ({run_id})')
            return {'runtime': runtime, 'runId': run_id, 'conversationId': conversation['id'], 'answer': result['answer']}
        time.sleep(2)
    request('/runs/' + run_id + '/cancel', user, {})
    raise TimeoutError(runtime + ' exceeded verification deadline')


def main():
    password = configuration()['BOOTSTRAP_ADMIN_PASSWORD']
    for user in ('a', 'b'):
        tokens[user] = request('/login', data={'username': user, 'password': password})['token']
    jobs = [
        ('a', 'pi', '请查询我2026年10月的应发工资，并从知识库说明工资何时发放。金额必须实时查HR，制度给出来源，用中文简短答复。'),
        ('b', 'dsh', '请查询我有权限仓库的HW-002工业轴承库存、预留数、可用量，并查询产品售价和保修期。只能使用实际工具数据与知识库，给出来源，用中文简短答复。'),
    ]
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(lambda args: run(*args), jobs))
    # Assertions are on seeded facts, not on exact natural-language wording.
    pi = results[0]['answer'].replace(',', '').replace('，', '')
    dsh = results[1]['answer'].replace(',', '').replace('，', '')
    assert '9000' in pi and '10' in pi, 'Pi answer did not contain the seeded payroll and payment date'
    assert all(value in dsh for value in ('42', '10', '32', '29', '12')), 'DSH answer missed seeded WMS/product/knowledge facts'
    assert '深圳' not in dsh and '180' not in dsh, 'DSH exposed an unassigned warehouse'
    (ROOT / '.local/agent-verification.json').write_text(json.dumps({'passed': True, 'results': results}, ensure_ascii=False, indent=2))
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
