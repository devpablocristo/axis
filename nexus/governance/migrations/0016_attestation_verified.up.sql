-- B.3: distinguir attestations criptográficamente verificadas de las que no.
-- Hasta que GOVERNANCE_ATTESTATION_VERIFIER esté configurado, todas las
-- attestations nuevas se persisten con verified=false y un verification_error
-- explicando el motivo. El caller (Companion u otros) puede leer el flag y
-- decidir si confiar.
--
-- Default false en attestations preexistentes: nunca fueron verificadas
-- criptográficamente y no podemos asumir que sí.

ALTER TABLE attestations ADD COLUMN IF NOT EXISTS verified BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE attestations ADD COLUMN IF NOT EXISTS verification_error TEXT NOT NULL DEFAULT '';
