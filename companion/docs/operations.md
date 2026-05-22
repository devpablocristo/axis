# Operations

Companion se opera desde la raíz de Axis. El servicio no tiene Docker, Compose,
Makefile ni env example propios.

## Local

```bash
# desde axis/
test -f .env || cp .env.example .env
make up
make logs
```

Para hot reload en host:

```bash
# desde axis/
docker compose up -d companion-postgres nexus-postgres nexus
make dev-companion
```

## Health

- `GET /healthz`: proceso vivo.
- `GET /readyz`: DB disponible.

## Migrations

El backend aplica migraciones embebidas al arrancar. Validar versiones con:

```bash
bash scripts/quality/check-migrations.sh
```

## Background Loops

- `COMPANION_NEXUS_SYNC_INTERVAL_SEC`: sync de tasks con Nexus.
- `COMPANION_STRICT_NEXUS`: activa fail-closed estricto para grants Nexus.
- `COMPANION_WATCHER_INTERVAL_SEC`: ejecución periódica de watchers.
- `COMPANION_WATCHER_SYNC_INTERVAL_SEC`: reconciliación de watcher proposals.
- Memory purge corre cada hora.

## Smoke

```bash
# desde axis/
make smoke-companion
```

Los smoke scripts esperan Companion y Nexus levantados, y usan las keys de
`.env` en la raíz de Axis.
