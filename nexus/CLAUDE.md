# Nexus

Nexus en Axis es un servicio headless de governance. No tiene UI propia; la UI
admin vive en `../console` y consume `../bff`.

## Reglas

- Mantener `governance/` como deployable independiente.
- No importar runtime LLM, memoria IA ni agentes.
- Auth inbound: `X-API-Key`, Bearer OIDC/JWKS o Bearer JWT interno de Axis BFF.
- Datos operativos tenant-owned requieren `org_id` no vacío.
- Config global solo para `policies`, `action_types` y `delegations`, protegida
  por `nexus:cross_org`.
- No agregar UI dentro de `nexus/`.

## Checks

```bash
make test
make qa
docker compose config --services
```
