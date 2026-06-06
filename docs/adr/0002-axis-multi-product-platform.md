# ADR 0002: Axis Multi-Product Platform

Status: accepted

## Context

Axis debe servir a N productos propios, no solamente a Ponti. Ponti sera el
primer piloto real, pero no debe condicionar el modelo base ni filtrar logica
vertical dentro de Companion, Nexus o el runtime compartido.

La decision mas importante es separar tres conceptos que suelen mezclarse:

- el cliente final que usa un producto;
- el producto conectado a Axis;
- la instalacion concreta de ese producto para ese cliente.

## Decision

El tenant real de Axis es la organizacion final del cliente:

- `org_id`: customer organization final.
- `product_surface`: producto conectado a Axis, por ejemplo `ponti`, `pymes`,
  `argos` o futuros productos.
- `product_installation`: instalacion concreta de `org_id + product_surface`.
- `external_tenant_id`: tenant/id interno del producto externo.
- `workspace`: JSON opaco que pertenece al producto y que Axis transporta sin
  interpretar como dominio propio.

Axis mantiene un product registry y un registry de installations. Companion,
Nexus y los componentes runtime deben operar usando el scope efectivo
`org_id + product_surface`.

## Contract Boundary

Los productos publican conocimiento mediante contratos:

- identity/JWT con `org_id`, `actor_id`, `product_surface`,
  `service_principal`, `on_behalf_of` y `scopes`;
- `capability_manifest.v1` para exponer read/write capabilities;
- `workspace` como contexto opaco del producto;
- secret/config references, nunca secretos planos.

Axis no hardcodea conocimiento vertical de Ponti, Pymes ni futuros productos.
Los productos pueden sembrar datos, manifests y assist packs, pero el runtime
solo consume contratos genericos.

## Tenancy

Los clientes de Ponti, Pymes u otros productos son los tenants de Axis.
Ponti no es tenant de Axis; Ponti es un `product_surface`.

Ejemplo:

```text
org_id = acme
product_surface = ponti
product_installation = acme + ponti
external_tenant_id = ponti-tenant-789
```

## Rejections

No aceptamos estos enfoques:

- usar `ponti` como default;
- modelar a Ponti como tenant real de Axis;
- guardar reglas agro dentro de Companion;
- permitir tool executions, memory writes o jobs sin `org_id` efectivo;
- guardar API keys, tokens o passwords en JSON de instalacion.

## Consequences

Un segundo producto puede conectarse sin redisenar tenancy. El costo es que
todos los runtime paths deben transportar y auditar `product_surface`, y las
integraciones deben pasar por registry/installations y conformance antes de
estar disponibles para agents o planners.

Ponti se integra despues como consumidor: publica capabilities, mapea sus
clientes a `org_id`, usa Companion via feature flag y delega writes sensibles a
Nexus.
