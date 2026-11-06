#!/usr/bin/env python3
"""Explicitly provision full Novex data access for the built-in admin account.

The normal HTTP API intentionally forbids editing the built-in account's roles.
This idempotent local maintenance command uses the configured application DB,
preserves all existing roles/passwords and records the new grant in the audit.
Run before starting test conversations: the policy trigger freezes old contexts.
"""
import json
import uuid

import psycopg

from local_config import configuration


def main():
    conf = configuration()
    if conf.get('DB_NAME') != 'novex-agent':
        raise RuntimeError('This command only provisions the local novex-agent database')
    with psycopg.connect(host=conf.get('DB_HOST', '127.0.0.1'), port=conf.get('DB_PORT', '5432'),
                         dbname=conf['DB_NAME'], user=conf['DB_USER'], password=conf['DB_PWD'],
                         sslmode=conf.get('DB_SSLMODE', 'disable'), connect_timeout=10) as conn:
        conn.execute('SET LOCAL lock_timeout=\'5s\'')
        conn.execute('LOCK TABLE sys_role, sys_user_role IN SHARE ROW EXCLUSIVE MODE')
        account = conn.execute("SELECT u.id FROM sys_user u JOIN novex_users n ON n.user_id=u.id WHERE u.username='admin' AND u.status=1 FOR UPDATE OF u").fetchone()
        if not account:
            raise RuntimeError('Existing enabled admin and Novex business profile are required')
        uid = account[0]
        admin_role = conn.execute("SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.id=ur.role_id WHERE ur.user_id=%s AND r.code='admin'", (uid,)).fetchone()
        if not admin_role:
            raise RuntimeError('The target account must already be a system administrator')
        role = conn.execute("SELECT id FROM sys_role WHERE code='novex_super_admin'").fetchone()
        if not role:
            role = conn.execute("INSERT INTO sys_role(id,name,code,data_scope,is_system,create_user,create_time) SELECT COALESCE(MAX(id),0)+1,'Novex 全局数据管理员','novex_super_admin',4,false,%s,now() FROM sys_role RETURNING id", (uid,)).fetchone()
        role_id = role[0]
        exists = conn.execute('SELECT 1 FROM sys_user_role WHERE user_id=%s AND role_id=%s', (uid, role_id)).fetchone()
        if not exists:
            conn.execute('INSERT INTO sys_user_role(id,user_id,role_id) SELECT COALESCE(MAX(id),0)+1,%s,%s FROM sys_user_role', (uid, role_id))
            conn.execute("INSERT INTO novex_audit(id,user_id,action,resource_id,allowed,detail) VALUES(%s,%s,'user.global_access.grant',%s,true,'Explicit local provision: all registered business resources and all knowledge documents; existing roles and password preserved')", (uuid.uuid4().hex, uid, str(uid)))
        version = conn.execute('SELECT version FROM novex_policy_version WHERE id=1').fetchone()[0]
    print(json.dumps({'username': 'admin', 'role': 'novex_super_admin', 'changed': not bool(exists), 'policyVersion': version}))


if __name__ == '__main__':
    main()
