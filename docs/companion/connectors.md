# Connectors

Un Connector es una instancia configurada para conectar Axis/Companion con un
producto o sistema externo.

No es una Capability. No es una Tool. No se asigna directamente a un Virployee.

```text
Connector -> descubre/ejecuta Tools
Tool -> funcion tecnica
Capability -> habilidad reusable
Virployee -> selecciona Capabilities
```

## Administracion

Console administra Connectors desde `Admin > Connectors` usando el mismo CRUD y
lifecycle que Virployees, Perfiles y Job Roles:

```text
Activos
Archivados
Papelera
```

La UI no pide JSON manual. Companion expone `ConnectorType.config_schema` y
Console renderiza los campos desde ese schema.

## Fuente Canonica

Los tipos de connector viven en Companion:

```text
GET /v1/connectors/types
GET /api/connectors/types
```

Cada `ConnectorType` define:

```text
kind
name
description
config_schema
supports_test
supports_refresh
status
```

## Configuracion

La configuracion se guarda tecnicamente en `config_json`, pero el usuario llena
campos simples. Secrets deben referenciarse con `secret_ref`; no se guardan
tokens o passwords crudos.

Para productos que implementan el contrato generico:

```text
kind = product-envelope-v1
base_url
secret_ref
auth_type
discovery_path
execute_path
external_tenant_id
timeout_ms
```

Defaults:

```text
auth_type = bearer
discovery_path = /api/v1/capabilities
execute_path = /api/v1/capability-executions
timeout_ms = 10000
```

## Endpoints

Companion:

```text
GET   /v1/connectors/types
GET   /v1/connectors?lifecycle=active|archived|trash|all
POST  /v1/connectors
GET   /v1/connectors/{connector_id}
PATCH /v1/connectors/{connector_id}
POST  /v1/connectors/{connector_id}/archive
POST  /v1/connectors/{connector_id}/trash
POST  /v1/connectors/{connector_id}/restore
POST  /v1/connectors/{connector_id}/test
POST  /v1/connectors/{connector_id}/refresh
GET   /v1/connectors/{connector_id}/executions
```

BFF:

```text
/api/connectors
```

## Fuera De Alcance

- CRUD libre de Tools.
- Asignar Connectors directamente a Virployees.
- Duplicar schemas de config en Console.
- Guardar secrets crudos.
- Cambios en Runtime.
