#!/usr/bin/env python3
"""Apply synthetic fixtures only to the dedicated Novex demo databases."""
import json
import os
import subprocess
from local_config import ROOT, configuration


def main():
    conf = configuration()
    path = ROOT / '.local/bootstrap.json'
    admin = json.loads(path.read_text()) if path.exists() else {}
    env = dict(os.environ)
    for name, field, default in [('PGHOST', 'host', '127.0.0.1'), ('PGPORT', 'port', 5432),
                                  ('PGUSER', 'user', 'postgres'), ('PGPASSWORD', 'password', '')]:
        env.setdefault(name, str(admin.get(field, default)))
    for database, fixture in [('novex-hr', 'hr.sql'), ('novex-hardware', 'hardware.sql')]:
        local = dict(env, PGDATABASE=database)
        subprocess.run(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-f', str(ROOT / 'fixtures' / fixture)], env=local, check=True)
        print(f'Applied synthetic fixture: {database}')
    subprocess.run(['python3', str(ROOT / 'scripts/bootstrap_databases.py')], check=True)
    goenv = dict(os.environ)
    goenv.update({k: v for k, v in conf.items() if not k.startswith('NOVEX_MODEL_')})
    subprocess.run(['go', 'run', './cmd/novex-seed'], cwd=ROOT / 'backend', env=goenv, check=True)


if __name__ == '__main__':
    main()
