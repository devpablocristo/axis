UPDATE companion_connectors
SET org_id = '__global_disabled__'
WHERE org_id = concat('__', 'leg', 'acy_global_disabled__');
