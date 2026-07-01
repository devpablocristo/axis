# Capabilities And Tools Domain Spec

## Proposito

Este spec separa `Capability` de `Tool`.

Regla:

```text
Capability = habilidad reusable
Tool = funcion tecnica invocable
```

Un `Virployee` referencia capabilities, no tools.

## Capability

Definicion: habilidad reusable declarada por contrato.

Utilidad: expresa que puede hacer un employee sin exponer la implementacion
tecnica exacta.

No representa: tool concreta, permiso real, approval ni job.

Tipo: entidad fuerte.

CRUD objetivo: si, via registry/import/promote.

Audiencia: admin/dev avanzado.

Modelo objetivo:

```text
Capability
- capability_id: UUID
- capability_key: string
- name: string
- description: string
- version: string
- product_id: UUID
- tool_id: UUID | null
- mode: CapabilityMode
- risk_class: RiskClass
- status: CapabilityStatus
```

Enums:

```text
CapabilityMode: read, write, execute
RiskClass: low, medium, high, critical
CapabilityStatus: draft, active, deprecated, blocked
```

Relaciones:

```text
Capability.product_id -> Product.product_id
Capability.tool_id -> Tool.tool_id | null
Virployee.capability_ids -> Capability.capability_id[]
JobRole.recommended_capability_ids -> Capability.capability_id[]
VirployeeProfile.default_capability_ids -> Capability.capability_id[]
```

Estado actual: existe `companion_capability_manifests` con `id uuid`,
`capability_id text`, `version`, `status`, `manifest_json`.

Brecha: `capability_id text` actual es realmente `capability_key`. El ID
fuerte objetivo debe ser UUID. El manifest flexible debe descomponerse en
campos de dominio o specs de contrato versionados.

## Tool

Definicion: funcion tecnica invocable por runtime o MCP.

Utilidad: ejecuta una operacion concreta.

No representa: habilidad de negocio reusable, employee, permiso ni policy.

Tipo: entidad fuerte tecnica.

CRUD objetivo: si tecnico, normalmente derivado de manifests o codigo.

Audiencia: interna/dev.

Modelo objetivo:

```text
Tool
- tool_id: UUID
- tool_key: string
- name: string
- description: string
- operation: string
- side_effect: boolean
- status: ToolStatus
```

Enums:

```text
ToolStatus: active, disabled, deprecated
```

Relaciones:

```text
Capability.tool_id -> Tool.tool_id | null
```

Estado actual: tools existen como funciones del runtime/MCP, no
como entidad publica clara.

Brecha: si se exponen como entidad, deben tener ID propio y no ser campos
sueltos dentro de Employee.

## Auditoria Actual Vs Objetivo

| Concepto actual | Problema | Modelo objetivo |
|---|---|---|
| `capability_id = billing.read` | Es key con puntos, no UUID. | `capability_key = billing.read`; `capability_id UUID`. |
| `allowed_tools` en agents/profiles | Mezcla implementacion tecnica con dominio employee. | Employee usa `capability_ids`; tools quedan tecnicas. |
| `manifest_json` | Contrato flexible sin forma de dominio en este spec. | Campos fuertes o contrato versionado documentado. |
| Tool runtime | No tiene frontera clara de entidad. | `Tool` tecnico con `tool_id` y `tool_key`. |
