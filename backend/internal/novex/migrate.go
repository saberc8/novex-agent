package novex

import "context"

func (s *Server) Migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS novex_policy_version(id integer PRIMARY KEY CHECK(id=1),version bigint NOT NULL DEFAULT 1);
INSERT INTO novex_policy_version(id,version) VALUES(1,1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS novex_users(user_id bigint PRIMARY KEY REFERENCES sys_user(id),tenant_id text NOT NULL,department_id text NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS novex_datasources(id text PRIMARY KEY,name text NOT NULL,host text NOT NULL,port integer NOT NULL,database_name text NOT NULL,username text NOT NULL,password_encrypted text NOT NULL,ssl_mode text NOT NULL,schema_snapshot jsonb NOT NULL DEFAULT '[]',created_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE novex_datasources ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS novex_resources(id text PRIMARY KEY,resource_key text NOT NULL UNIQUE,name text NOT NULL,datasource_id text NOT NULL REFERENCES novex_datasources(id),config jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS novex_identities(id text PRIMARY KEY,user_id bigint NOT NULL REFERENCES sys_user(id),datasource_id text NOT NULL REFERENCES novex_datasources(id),tenant_id text NOT NULL,employee_id text NOT NULL DEFAULT '',department_id text NOT NULL DEFAULT '',warehouse_ids jsonb NOT NULL DEFAULT '[]',UNIQUE(user_id,datasource_id));
ALTER TABLE novex_identities ADD COLUMN IF NOT EXISTS external_tenant_id text NOT NULL DEFAULT '';
UPDATE novex_identities SET external_tenant_id=tenant_id WHERE external_tenant_id='';
CREATE TABLE IF NOT EXISTS novex_grants(id text PRIMARY KEY,role_id bigint NOT NULL REFERENCES sys_role(id),resource_id text NOT NULL REFERENCES novex_resources(id),scope text NOT NULL CHECK(scope IN ('self','department','warehouse','tenant')),fields jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS novex_documents(id text PRIMARY KEY,title text NOT NULL,file_name text NOT NULL,scope text NOT NULL CHECK(scope IN ('self','department','tenant')),owner_id bigint NOT NULL REFERENCES sys_user(id),tenant_id text NOT NULL,department_id text NOT NULL,markdown text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE novex_documents ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'markdown';
ALTER TABLE novex_documents ADD COLUMN IF NOT EXISTS original bytea;
ALTER TABLE novex_documents ADD COLUMN IF NOT EXISTS sheet_count integer NOT NULL DEFAULT 0;
ALTER TABLE novex_documents ADD COLUMN IF NOT EXISTS row_count integer NOT NULL DEFAULT 0;
ALTER TABLE novex_documents ADD COLUMN IF NOT EXISTS warnings jsonb NOT NULL DEFAULT '[]';
CREATE TABLE IF NOT EXISTS novex_workbook_sheets(document_id text NOT NULL REFERENCES novex_documents(id) ON DELETE CASCADE,name text NOT NULL,header_row integer NOT NULL,columns jsonb NOT NULL,row_count integer NOT NULL DEFAULT 0,PRIMARY KEY(document_id,name));
CREATE TABLE IF NOT EXISTS novex_workbook_rows(document_id text NOT NULL,sheet_name text NOT NULL,row_number integer NOT NULL,cells jsonb NOT NULL,formulas jsonb NOT NULL DEFAULT '{}',search_text text NOT NULL,PRIMARY KEY(document_id,sheet_name,row_number),FOREIGN KEY(document_id,sheet_name) REFERENCES novex_workbook_sheets(document_id,name) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS novex_workbook_cells_idx ON novex_workbook_rows USING gin(cells jsonb_path_ops);
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS novex_workbook_search_idx ON novex_workbook_rows USING gin(search_text gin_trgm_ops);
CREATE INDEX IF NOT EXISTS novex_documents_text_idx ON novex_documents USING gin(markdown gin_trgm_ops);
CREATE INDEX IF NOT EXISTS novex_documents_title_idx ON novex_documents USING gin(title gin_trgm_ops);
CREATE TABLE IF NOT EXISTS novex_conversations(id text PRIMARY KEY,user_id bigint NOT NULL REFERENCES sys_user(id),tenant_id text NOT NULL,title text NOT NULL,runtime text NOT NULL,policy_version bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS novex_runs(id text PRIMARY KEY,conversation_id text NOT NULL REFERENCES novex_conversations(id),user_id bigint NOT NULL,tenant_id text NOT NULL,policy_version bigint NOT NULL,status text NOT NULL DEFAULT 'queued',token_hash text NOT NULL,answer text NOT NULL DEFAULT '',error text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS novex_messages(id text PRIMARY KEY,conversation_id text NOT NULL REFERENCES novex_conversations(id),run_id text NOT NULL DEFAULT '',role text NOT NULL,content text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS novex_audit(id text PRIMARY KEY,user_id bigint NOT NULL,action text NOT NULL,resource_id text NOT NULL DEFAULT '',allowed boolean NOT NULL,detail text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS novex_embed_tickets(token_hash text PRIMARY KEY,user_id bigint NOT NULL,parent_origin text NOT NULL,expires_at timestamptz NOT NULL,used boolean NOT NULL DEFAULT false);
CREATE INDEX IF NOT EXISTS novex_runs_user_idx ON novex_runs(user_id,created_at);
CREATE INDEX IF NOT EXISTS novex_messages_conversation_idx ON novex_messages(conversation_id,created_at);
CREATE INDEX IF NOT EXISTS novex_audit_time_idx ON novex_audit(created_at);
INSERT INTO novex_users(user_id,tenant_id,department_id) SELECT id,'acme',dept_id::text FROM sys_user WHERE username='admin' ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION novex_bump_policy() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE novex_policy_version SET version=version+1 WHERE id=1; RETURN NULL; END $$;
DO $$ DECLARE table_name text; BEGIN
 FOREACH table_name IN ARRAY ARRAY['sys_user','sys_user_role','sys_role','sys_dept','novex_users','novex_datasources','novex_resources','novex_identities','novex_grants','novex_documents'] LOOP
 IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='novex_policy_'||table_name) THEN
 EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION novex_bump_policy()', 'novex_policy_'||table_name,table_name);
 END IF;
 END LOOP;
END $$;

`)
	if err != nil {
		return err
	}
	return s.migrateMenus(context.Background())
}
