-- governance_role_assignments: roles internos de governance para usuarios de
-- organizaciones consumidoras (pymes y otros).
--
-- Modelo: governance NO duplica `orgs` ni `users` de los SaaS consumidores.
-- Solo guarda QUÉ ROL DE GOVERNANCE tiene cada (org_id, user_id) externo.
-- `org_id` y `user_id` son identificadores opacos (provienen del JWT/IdP del
-- consumidor). Sin FK referencial.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS governance_role_assignments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      text NOT NULL,
    user_id     text NOT NULL,
    role        text NOT NULL CHECK (role IN ('policy_admin', 'approver', 'auditor', 'delegate')),
    granted_by  text,
    granted_at  timestamptz NOT NULL DEFAULT now(),
    revoked_at  timestamptz,
    UNIQUE (org_id, user_id, role)
);

CREATE INDEX IF NOT EXISTS idx_governance_role_assignments_org_user
    ON governance_role_assignments (org_id, user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_governance_role_assignments_role
    ON governance_role_assignments (org_id, role)
    WHERE revoked_at IS NULL;
