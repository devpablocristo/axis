-- Reverse 0039: re-impose NOT NULL + non-empty CHECK on the contract columns.
-- Any NULL/empty values written while relaxed are backfilled to a placeholder so
-- the CHECK can be re-added.

UPDATE assist_packs SET input_contract = 'unknown' WHERE input_contract IS NULL OR btrim(input_contract) = '';
UPDATE assist_packs SET output_contract = 'unknown' WHERE output_contract IS NULL OR btrim(output_contract) = '';
ALTER TABLE assist_packs
	ALTER COLUMN input_contract SET NOT NULL,
	ALTER COLUMN output_contract SET NOT NULL,
	ADD CONSTRAINT assist_packs_input_contract_check CHECK (btrim(input_contract) <> ''),
	ADD CONSTRAINT assist_packs_output_contract_check CHECK (btrim(output_contract) <> '');

UPDATE assist_pack_versions SET input_contract = 'unknown' WHERE input_contract IS NULL OR btrim(input_contract) = '';
UPDATE assist_pack_versions SET output_contract = 'unknown' WHERE output_contract IS NULL OR btrim(output_contract) = '';
ALTER TABLE assist_pack_versions
	ALTER COLUMN input_contract SET NOT NULL,
	ALTER COLUMN output_contract SET NOT NULL,
	ADD CONSTRAINT assist_pack_versions_input_contract_check CHECK (btrim(input_contract) <> ''),
	ADD CONSTRAINT assist_pack_versions_output_contract_check CHECK (btrim(output_contract) <> '');
