-- Permitir borrar policies sin romper requests que las referencian.
ALTER TABLE requests DROP CONSTRAINT IF EXISTS requests_policy_id_fkey;
ALTER TABLE requests ADD CONSTRAINT requests_policy_id_fkey
    FOREIGN KEY (policy_id) REFERENCES policies(id) ON DELETE SET NULL;
