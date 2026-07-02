DELETE FROM action_types
WHERE org_id IS NULL
  AND name IN (
      'agent.capability.invoke',
      'agent.capability.compensate'
  );
