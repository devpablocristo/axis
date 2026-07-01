# Nexus Integration

Companion consume Nexus como sistema de decisiones sensibles. Nexus decide; Companion
obedece.

## Contrato operacional

1. Companion detecta intención o capability sensible.
2. Companion construye `ToolIntent v1` con `schema_version`, customer org
   (`org_id`), actor humano/delegado, `companion_principal`, product surface,
   capability, operación, target, `payload_hash`, `idempotency_key`,
   `run_id` y `tool_invocation_id`.
3. Companion envía el intent a Nexus como `action_binding`; Nexus calcula y
   persiste `binding_hash`.
4. Nexus responde decisión/estado y `binding_hash`.
5. Companion persiste `nexus_request_id` y ejecuta solo si el hash local
   coincide con el hash aprobado por Nexus.
6. Companion reporta resultado cuando aplica.

## Estado actual

- Tasks y runtime consultan Nexus antes de ejecutar writes controladas.
- Las acciones sensibles requieren `org_id`, `actor_id`, `idempotency_key` y
  `binding_hash` válido.
- Evidence y traces distinguen `actor_id`/`human_user_id`/`on_behalf_of` de la
  identidad tecnica `companion.employee_ai`.
- Watcher proposals tienen loop de reconciliación para decisiones pendientes.
- Nexus assist requiere scopes dedicados.

## Reglas

- Companion no evalúa CEL/policies.
- Companion no duplica risk engine.
- Companion no aprueba/rechaza como actor autónomo del LLM.
- Cada ejecución sensible debe tener correlation con Nexus y evidence
  sanitizada.
- Una approval no puede reutilizarse para otra operación/payload: el
  `binding_hash` debe coincidir con la acción real.
