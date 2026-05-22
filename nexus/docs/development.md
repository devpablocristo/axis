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
