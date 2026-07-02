-- input_contract/output_contract are being removed from the assist-pack model:
-- they were stored but never validated or read at runtime (validatePack only
-- checked non-empty; runLLM/RunAssist never read them). Step 1 of the removal:
-- relax the NOT NULL + non-empty CHECK on assist_packs AND its version-history
-- table so packs can be published without contracts without breaking inserts or
-- the save_assist_pack_previous_version trigger. The columns themselves are
-- dropped in a later migration once no writer references them.

ALTER TABLE assist_packs
	ALTER COLUMN input_contract DROP NOT NULL,
	ALTER COLUMN output_contract DROP NOT NULL,
	DROP CONSTRAINT IF EXISTS assist_packs_input_contract_check,
	DROP CONSTRAINT IF EXISTS assist_packs_output_contract_check;

ALTER TABLE assist_pack_versions
	ALTER COLUMN input_contract DROP NOT NULL,
	ALTER COLUMN output_contract DROP NOT NULL,
	DROP CONSTRAINT IF EXISTS assist_pack_versions_input_contract_check,
	DROP CONSTRAINT IF EXISTS assist_pack_versions_output_contract_check;
