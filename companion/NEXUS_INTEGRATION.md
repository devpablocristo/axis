# Nexus Integration

Companion consume Nexus como sistema de gobernanza. Nexus decide; Companion
obedece.

## Contrato operacional

1. Companion detecta intención o capability sensible.
2. Companion construye `ToolIntent v1` con `schema_version`, tenant, actor,
   product surface, connector/capability, operación, target, `payload_hash`,
   `idempotency_key`, `run_id` y `tool_invocation_id`.
3. Companion envía el intent a Nexus como `action_binding`; Nexus calcula y
   persiste `binding_hash`.
4. Nexus responde decisión/estado y `binding_hash`.
5. Companion persiste `governance_request_id` y ejecuta solo si el hash local
   coincide con el hash aprobado por Nexus.
6. Companion reporta resultado cuando aplica.

## Estado actual

- Tasks y connectors consultan Nexus antes de ejecutar writes gobernadas.
- Connectors aceptan `allowed`, `approved` y `executed` como estados que
  habilitan ejecución.
- Connectors rechazan side effects sin `org_id`, `actor_id`,
  `idempotency_key` y `binding_hash` válido.
- Watcher proposals tienen loop de reconciliación para decisiones pendientes.
- Governance assist requiere scopes dedicados.

## Reglas

- Companion no evalúa CEL/policies.
- Companion no duplica risk engine.
- Companion no aprueba/rechaza como actor autónomo del LLM.
- Cada ejecución sensible debe tener correlation con Nexus y evidence
  sanitizada.
- Una approval no puede reutilizarse para otra operación/payload: el
  `binding_hash` debe coincidir con la acción real.
