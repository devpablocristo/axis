# Nexus Development

Nexus en Axis es un servicio headless para decisiones sensibles. No tiene UI
propia; la UI admin vive en `../console` y consume `../bff`.

## Reglas del Servicio

- Mantener Nexus como deployable independiente dentro de Axis.
- No importar runtime LLM, memoria IA ni agentes.
- Auth inbound: `X-API-Key`, Bearer OIDC/JWKS o Bearer JWT interno de Axis BFF.
- Datos operativos tenant-owned requieren `org_id` no vacío.
- Config global solo para `policies`, `action_types` y `delegations`,
  protegida por `nexus:cross_org`.
- No agregar UI dentro de `nexus/`.

## Go

- Código en inglés; comentarios y docs operativas en español.
- `context.Context` como primer parámetro en I/O.
- No usar `panic()`, `_` para errores, `fmt.Printf` para logging ni
  `err.Error()` en respuestas HTTP.
- DTOs HTTP en `handler/dto/dto.go`; no exponer structs de dominio por HTTP.
- Repositories Postgres; no repositories in-memory de producción.
- Fakes inline en `_test.go`.

## Checks

```bash
cd .. && make test-nexus
cd .. && make qa-nexus
docker compose config --services
```

## Admins locales para Separation of Duties

El stack local mantiene `NEXUS_ADMIN_API_KEY` como admin legacy/cross-org para
compatibilidad, pero las pruebas de policy promotion usan identidades separadas:

- `NEXUS_ADMIN_A_API_KEY` resuelve actor `nexus-admin-a` en `AXIS_DEV_ORG_ID`.
- `NEXUS_ADMIN_B_API_KEY` resuelve actor `nexus-admin-b` en `AXIS_DEV_ORG_ID`.
- `NEXUS_OTHER_ORG_ADMIN_API_KEY` resuelve actor `nexus-admin-other` en
  `AXIS_OTHER_ORG_ID`.

Los actores se derivan de la metadata `actor=` de `NEXUS_API_KEYS`. Una policy
promotion solicitada por Admin A debe rechazar self-approval con `409`; Admin B
puede aprobarla porque tiene un actor distinto en la misma org.

```bash
cd .. && make e2e-nexus-policy-promotion
cd .. && make e2e-nexus
```
