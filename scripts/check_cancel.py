#!/usr/bin/env python3
"""Verify cancellation of a real Docker harness run, including bounded cleanup."""
import json
import subprocess
import time
import urllib.request
from local_config import ROOT, configuration
from check_api import request, tokens


def containers():
    return subprocess.check_output(['docker', 'ps', '--filter', 'name=novex-run-', '--format', '{{.Names}}'], text=True).strip()


def main():
    if containers():
        raise SystemExit('Run this check when no other demonstration is active.')
    config = configuration()
    tokens['a'] = request('/login', data={'username': 'a', 'password': config['BOOTSTRAP_ADMIN_PASSWORD']})['token']
    conv = request('/conversations', 'a', {'runtime': 'dsh', 'title': '取消与清理验收'})
    rid = request('/conversations/' + conv['id'] + '/messages', 'a', {'content': '请详细核对全部可访问产品资料及本人2026年10月工资，说明依据。'})['runId']
    started = False
    for _ in range(80):
        if containers():
            started = True
            break
        state = request('/runs/' + rid, 'a')
        if state['status'] in ('completed', 'error', 'cancelled'):
            break
        time.sleep(.2)
    request('/runs/' + rid + '/cancel', 'a', {})
    # The worker grants two seconds for graceful teardown before forced removal.
    # Do not mistake the detached docker client's exit for completed cleanup.
    for _ in range(75):
        state = request('/runs/' + rid, 'a')
        with urllib.request.urlopen('http://127.0.0.1:8320/health') as response:
            health = json.load(response)
        remaining = containers()
        if state['status'] in ('cancelled', 'error') and health['activeRuns'] == 0 and not remaining:
            break
        time.sleep(.2)
    assert started, 'Runtime container did not start before cancellation'
    assert state['status'] in ('cancelled', 'error') and not state.get('answer'), 'Cancelled answer must not publish'
    assert health['activeRuns'] == 0 and not remaining, 'Runtime did not clean up within the bounded grace period'
    report = {'passed': True, 'runtime': 'dsh', 'containerStarted': True, 'finalStatus': state['status'], 'answerEmpty': True, 'activeRuns': 0, 'containersRemaining': False}
    (ROOT / '.local/cancel-verification.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report))


if __name__ == '__main__':
    main()
