#!/usr/bin/env python3
"""Start/stop local Novex services, retaining logs and PID records privately."""
import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import time
from local_config import ROOT, configuration

STATE = ROOT / '.local/processes.json'
SERVICES = ('model', 'worker', 'api', 'chat', 'admin')


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


def load_state():
    return json.loads(STATE.read_text()) if STATE.exists() else {}


def start(name, state, config):
    if name in state and alive(state[name]['pid']):
        print(f'{name}: already running (PID {state[name]["pid"]})')
        return
    node = ROOT / 'agent-worker/node_modules/node/bin/node'
    inherited = {key: value for key, value in os.environ.items() if key in ('PATH', 'HOME', 'LANG', 'LC_ALL', 'TMPDIR', 'USER')}
    env = dict(inherited)
    cwd = ROOT
    if name == 'model':
        env.update({k: v for k, v in config.items() if k.startswith('NOVEX_MODEL_') or k == 'NOVEX_RUNNER_SECRET'})
        if config.get('NOVEX_HTTPS_PROXY'):
            env['HTTPS_PROXY'] = config['NOVEX_HTTPS_PROXY']
            env['HTTP_PROXY'] = config['NOVEX_HTTPS_PROXY']
        env['NO_PROXY'] = 'localhost,127.0.0.1,host.docker.internal'
        command = [str(node), '--use-env-proxy', str(ROOT / 'scripts/model-proxy.mjs')]
    elif name == 'worker':
        env.update({k: v for k, v in config.items() if k.startswith('NOVEX_') and k not in ('NOVEX_MODEL_API_KEY', 'NOVEX_MASTER_KEY', 'NOVEX_ENCRYPTION_KEY', 'NOVEX_HR_PASSWORD', 'NOVEX_HARDWARE_PASSWORD')})
        env['NOVEX_MODEL_PROXY_URL'] = 'http://127.0.0.1:8330'
        cwd = ROOT / 'agent-worker'
        command = [str(node), str(cwd / 'src/server.js')]
    elif name == 'api':
        env.update({k: v for k, v in config.items() if k.startswith(('DB_', 'REDIS_', 'AUTH_', 'HTTP_', 'BOOTSTRAP_')) or k in ('APP_ENV', 'GIN_MODE', 'NOVEX_ENCRYPTION_KEY', 'NOVEX_MASTER_KEY', 'NOVEX_DATA_DIR', 'NOVEX_RUNNER_URL', 'NOVEX_RUNNER_SECRET', 'NOVEX_PUBLIC_URL', 'NOVEX_PUBLIC_API_URL', 'NOVEX_EMBED_ORIGINS', 'NOVEX_EMBED_PARENT_ORIGINS', 'NOVEX_CHAT_ORIGIN', 'NOVEX_CHAT_ORIGINS', 'NOVEX_PYTHON', 'NOVEX_CONVERTER_SCRIPT')})
        cwd = ROOT / 'backend'
        binary = ROOT / '.local/bin/novex-api'
        binary.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(['go', 'build', '-o', str(binary), './cmd/admin'], cwd=cwd, env=env, check=True)
        command = [str(binary)]
    else:
        cwd = ROOT / name
        env['BROWSER'] = 'none'
        command = [str(node), str(cwd / 'node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '8312' if name == 'chat' else '8311', '--strictPort']
    logfile = ROOT / '.local/logs' / (name + '.log')
    logfile.parent.mkdir(parents=True, exist_ok=True)
    with logfile.open('ab') as stream:
        proc = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
    state[name] = {'pid': proc.pid, 'started': time.time(), 'log': str(logfile), 'command': command}
    STATE.write_text(json.dumps(state, indent=2))
    print(f'{name}: started (PID {proc.pid})')


def stop(name, state):
    entry = state.get(name)
    if entry and alive(entry['pid']):
        # Only terminate a process that still matches the recorded executable,
        # rather than risking a recycled PID after a machine restart.
        actual = subprocess.run(['ps', '-p', str(entry['pid']), '-o', 'command='], text=True, capture_output=True).stdout.strip()
        expected = Path(entry['command'][0]).name
        if expected not in actual and 'pnpm' not in actual:
            raise RuntimeError(f'{name}: PID no longer matches our service')
        os.killpg(entry['pid'], signal.SIGTERM)
    state.pop(name, None)
    STATE.write_text(json.dumps(state, indent=2))
    print(f'{name}: stopped')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('start', 'stop', 'restart', 'status'))
    parser.add_argument('services', nargs='*', choices=(*SERVICES, 'all'), default=['all'])
    args = parser.parse_args()
    selected = SERVICES if not args.services or 'all' in args.services else args.services
    state = load_state()
    config = configuration()
    for name in selected:
        if args.action == 'status':
            entry = state.get(name, {})
            print(f'{name}: ' + ('running' if entry.get('pid') and alive(entry['pid']) else 'stopped'))
        else:
            if args.action in ('stop', 'restart'):
                stop(name, state)
            if args.action in ('start', 'restart'):
                start(name, state, config)


if __name__ == '__main__':
    main()
