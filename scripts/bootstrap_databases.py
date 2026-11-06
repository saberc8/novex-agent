#!/usr/bin/env python3
"""Create only the three dedicated Novex databases and least-privilege roles.

Administrative PostgreSQL credentials come from PG* environment variables or
the ignored .local/bootstrap.json. Existing unrelated databases are untouched.
"""
import json
import os
import subprocess
from local_config import ROOT, configuration


def ident(value):
    return '"' + value.replace('"', '""') + '"'


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def main():
    conf = configuration()
    private = ROOT / '.local/bootstrap.json'
    admin = json.loads(private.read_text()) if private.exists() else {}
    env = dict(os.environ)
    for name, field, default in [('PGHOST', 'host', '127.0.0.1'), ('PGPORT', 'port', 5432),
                                  ('PGUSER', 'user', 'postgres'), ('PGPASSWORD', 'password', ''),
                                  ('PGDATABASE', 'dbname', 'postgres')]:
        env.setdefault(name, str(admin.get(field, default)))

    def sql(statement, database=None):
        callenv = dict(env)
        if database:
            callenv['PGDATABASE'] = database
        result = subprocess.run(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At'],
                                input=statement, text=True, capture_output=True, env=callenv)
        if result.returncode:
            # PostgreSQL can echo statements containing role passwords. Keep
            # failures terse; inspect the server privately when diagnosing.
            raise RuntimeError('PostgreSQL bootstrap failed; no credentials were printed.')
        return result.stdout.strip()

    roles = [(conf['DB_USER'], conf['DB_PWD']),
             (conf['NOVEX_HR_USER'], conf['NOVEX_HR_PASSWORD']),
             (conf['NOVEX_HARDWARE_USER'], conf['NOVEX_HARDWARE_PASSWORD'])]
    for name, password in roles:
        if not name.startswith('novex_'):
            raise ValueError('Bootstrap only manages novex_ roles')
        if not sql(f'SELECT 1 FROM pg_roles WHERE rolname={literal(name)};'):
            sql(f'CREATE ROLE {ident(name)} LOGIN PASSWORD {literal(password)} NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;')
        else:
            sql(f'ALTER ROLE {ident(name)} PASSWORD {literal(password)};')
    if not sql("SELECT 1 FROM pg_roles WHERE rolname='novex_data_owner';"):
        sql('CREATE ROLE novex_data_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;')

    databases = [('novex-agent', conf['DB_USER']), ('novex-hr', 'novex_data_owner'),
                 ('novex-hardware', 'novex_data_owner')]
    for name, owner in databases:
        if not sql(f'SELECT 1 FROM pg_database WHERE datname={literal(name)};'):
            sql(f'CREATE DATABASE {ident(name)} OWNER {ident(owner)};')
        sql(f'REVOKE CONNECT,TEMPORARY ON DATABASE {ident(name)} FROM PUBLIC; GRANT CONNECT ON DATABASE {ident(name)} TO {ident(owner)};')
        sql('REVOKE CREATE ON SCHEMA public FROM PUBLIC;', name)
        if name == 'novex-agent':
            sql(f'GRANT USAGE,CREATE ON SCHEMA public TO {ident(owner)};', name)
        print(f'Ready: {name}')

    for database, role in [('novex-hr', conf['NOVEX_HR_USER']), ('novex-hardware', conf['NOVEX_HARDWARE_USER'])]:
        sql(f'GRANT CONNECT ON DATABASE {ident(database)} TO {ident(role)};')
        sql(f'GRANT USAGE ON SCHEMA public TO {ident(role)}; GRANT SELECT ON ALL TABLES IN SCHEMA public TO {ident(role)}; '
            f'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO {ident(role)}; '
            f'ALTER DEFAULT PRIVILEGES FOR ROLE novex_data_owner IN SCHEMA public GRANT SELECT ON TABLES TO {ident(role)};', database)
        sql(f'ALTER ROLE {ident(role)} SET default_transaction_read_only=on; ALTER ROLE {ident(role)} SET statement_timeout=5000;')
    print('Dedicated login roles are configured. Apply fixture SQL, then rerun to grant SELECT on new tables.')


if __name__ == '__main__':
    main()
