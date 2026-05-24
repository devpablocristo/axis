# AGENTS.md

Guia operativa para agentes que trabajen en este repo. Este archivo fue creado
porque no existia un `AGENTS.md` previo en la raiz de Axis; no reemplaza
contenido anterior.

## Orden de lectura

1. Leer este archivo completo.
2. Leer `PROJECT_CONTEXT.md`.
3. Leer `README.md`.
4. Leer el README y los docs del servicio que se vaya a tocar:
   - `companion/README.md` y `companion/docs/`
   - `nexus/README.md` y `nexus/docs/`
   - `bff/README.md`
   - `console/README.md`
   - `packages/README.md`

## Reglas de arquitectura

- Axis es un monorepo con deployables independientes. Compartir repo no implica
  compartir runtime, base de datos, secrets ni ciclo de deploy.
- El browser habla con `console`; `console` habla con `bff`; `bff` habla por
  HTTP con `companion` y `nexus`.
- `companion` y `nexus` no tienen UI propia como runtime productivo. La
  administracion operativa vive en `console` via `bff`.
- `companion` concentra IA, agentes, runtime LLM, tools, memoria, watchers y
  ejecucion asistida.
- `nexus` decide acciones sensibles: `allow`, `deny` o `require_approval`.
  Administra approvals, policies, action types, delegations, RBAC, audit y
  evidence.
- `companion` no evalua policies, no aprueba ni rechaza requests como motor de
  decision, y no reimplementa el risk engine de `nexus`.
- `nexus` no debe importar runtime LLM, memoria IA ni agentes.
- Productos externos exponen capacidades de dominio; Axis no debe hardcodear
  conocimiento vertical de producto en `companion` o `nexus`.

## Imports y boundaries

- La comunicacion entre servicios es HTTP mas contratos compartidos.
- No importar internals de otro servicio. En particular, nada fuera de
  `nexus/` debe importar `github.com/devpablocristo/nexus/internal/...`.
- `packages/contracts` existe para contratos compartidos; no usarlo como atajo
  para compartir logica interna de servicios.
- `packages/auth` contiene helpers de identidad interna, scopes, tenants y
  token exchange. En Companion, `org_id` debe leerse semanticamente como
  `customer_org_id`: la empresa cliente donde trabaja el empleado IA. Los
  servicios productivos siguen validando identidad en sus propios boundaries.
- `packages/ui` contiene componentes visuales compartidos para `console`, sin
  logica pesada de dominio de Nexus o Companion.

## Desarrollo local

Desde la raiz del repo:

```bash
make test
make qa
docker compose config --services
```

Para levantar el stack local:

```bash
test -f .env || cp .env.example .env
docker compose up -d --build
```

Para hot reload de APIs:

```bash
docker compose up -d companion-postgres nexus-postgres
make dev-apis
```

Comandos por componente:

```bash
make test-companion
make qa-companion
make smoke-companion
make test-nexus
make qa-nexus
make smoke-nexus
make e2e-nexus
make test-bff
make test-console
```

## Calidad y checks

- `make hygiene` valida reglas de estructura y textos prohibidos del repo.
- `check-companion` corre checks de migrations, imports de Nexus y pipeline de
  side effects.
- `check-nexus` corre checks de migrations y ausencia de runtime IA en Nexus.
- `companion` y `nexus` embeben migraciones al arrancar; agregar siempre pares
  `*.up.sql` y `down/*.down.sql` con version unica.
- Los tests no deben requerir LLM real. Usar fakes/mocks para Nexus, productos
  y providers cuando corresponda.
- Para `console`, usar Node `22.12.0` segun `console/.nvmrc`; los scripts
  relevantes son `npm run typecheck` y `npm run build`.

## Convenciones de codigo

- En Go, usar `context.Context` como primer parametro en operaciones de I/O.
- No usar `panic()` para errores recuperables, no ignorar errores con `_`, no
  usar `fmt.Printf` para logging productivo y no exponer `err.Error()` crudo en
  respuestas HTTP.
- En Nexus, DTOs HTTP viven en `handler/dto/dto.go`; no exponer structs de
  dominio por HTTP.
- Repositories productivos son Postgres; fakes inline en `_test.go`.
- Codigo en ingles; documentacion operativa en espanol donde el repo ya sigue
  ese estilo.

## Seguridad, customer orgs y secretos

- `companion` y `nexus` validan identidad y `org_id` por si mismos; el BFF no
  es el unico boundary multi-tenant.
- El BFF puede seleccionar la organizacion efectiva con `X-Axis-Org-ID` cuando
  el principal tiene scope cross-org, y firma Bearer JWT interno acotado a ese
  `org_id` para `companion` y `nexus`.
- Companion interpreta `org_id` como customer org/work context, no como
  ownership administrativo del runtime global. Solo Axis/owner administra
  prompts, modelos, autonomia, perfiles y capabilities globales.
- Datos operativos de una customer org requieren `org_id` no vacio.
- `tenant` queda como termino historico/de compatibilidad en nombres de campos,
  packages o datos ya existentes; no usarlo como concepto nuevo en Companion.
- No commitear `.env`, API keys, bearer tokens ni payloads sensibles.
- Evidence y logs deben sanitizar secretos conocidos.
- No registrar secrets ni valores de Secret Manager. Documentar nombres de
  variables o secrets, no valores reales.

## Trabajo en repo con cambios locales

- Puede haber worktree dirty. No revertir cambios ajenos.
- Antes de editar archivos existentes, leerlos completos o la seccion necesaria
  y preservar informacion util.
- No reescribir docs de contexto desde cero si ya existen; fusionar y mover
  dudas a una seccion "Posiblemente obsoleto / pendiente de confirmar".
- Mantener cambios acotados al pedido y evitar refactors no solicitados.

## Cambios de este archivo

- Creado como guia nueva porque `AGENTS.md` no existia en la raiz de Axis al
  momento de implementacion.
- Actualizado para reflejar la identidad canonica de Companion: BFF selecciona
  org con `X-Axis-Org-ID`, Companion trata `org_id` como customer org de trabajo
  y `tenant` queda como compatibilidad historica.
- Fuentes usadas: `README.md`, READMEs por servicio, `Makefile`,
  `docker-compose.yml`, `.env.example`, workflows de `.github/workflows/`,
  `companion/docs/`, `nexus/docs/development.md` y docs compartidos en
  `packages/`.
