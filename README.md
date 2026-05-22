# Axis

Monorepo de control operativo para IA, decisiones sensibles y consola admin/ops.

Axis agrupa deployables independientes. Vivir en el mismo repo no implica mismo
runtime, misma base de datos ni mismo ciclo de deploy.

## Estructura

```text
axis/
├── companion/   # servicio headless de IA/runtime/tools/memory
├── nexus/       # servicio headless de policies/approvals/audit
├── bff/         # backend-for-frontend de la consola operativa
├── console/     # UI admin/ops de Axis
└── packages/    # contratos, auth, UI compartida
```

## Deployables

| Carpeta | Deployable | Rol |
|---|---|---|
| `companion/` | `axis-companion` | API headless IA |
| `nexus/` | `axis-nexus` | API headless de decisiones sensibles |
| `bff/` | `axis-bff` | HTTP BFF para `console/` |
| `console/` | `axis-console` | UI admin/ops |

## Reglas

- `companion` y `nexus` no poseen UI propia como runtime productivo.
- El browser habla con `console`; `console` habla con `bff`; `bff` habla por
  HTTP con `companion` y `nexus`.
- Cada deployable mantiene sus env vars, secrets, DB y pipeline.
- Los imports directos entre internals de servicios quedan prohibidos; la
  comunicación entre servicios es HTTP + contratos compartidos.
- `companion` y `nexus` validan identidad y `org_id` por sí mismos; el BFF no
  es el único boundary de multi-tenancy.
- Releases por componente: `companion-v*`, `nexus-v*`, `bff-v*`,
  `console-v*`.

## Desarrollo local

```bash
make test
docker compose config --services
```

Para levantar todo el stack local:

```bash
test -f .env || cp .env.example .env
docker compose up -d --build
```

Para desarrollo con hot reload de APIs:

```bash
docker compose up -d companion-postgres nexus-postgres
make dev-apis
```

Puertos por defecto:

| Servicio | URL |
|---|---|
| Axis Console | `http://localhost:13000` |
| Axis BFF | `http://localhost:18080` |
| Companion API | `http://localhost:18085` |
| Nexus API | `http://localhost:18084` |
