# Boundaries

## Regla principal

IA vive en Companion. Decisiones sensibles vive en Nexus. Dominio vertical vive en los
productos.

## Companion

Companion puede:

- Orquestar LLMs, agents, tools y memoria.
- Decidir qué capability quiere invocar.
- Preparar evidence y contexto operativo.
- Consultar Nexus antes de acciones sensibles.
- Persistir traces operativas del runtime IA.
- Exponer APIs headless para productos, gateways, BFFs y servicios internos.

Companion no puede:

- Evaluar policies.
- Aprobar o rechazar requests como motor de nexus.
- Reimplementar risk engine o audit fuerte.
- Guardar memoria sin customer org/user/product context cuando aplique.
- Mezclar datos entre customer orgs.
- Poseer una UI de usuario final o una console propia como parte de su runtime.

## Nexus

Nexus decide `allow`, `deny` o `require_approval`, administra approvals,
policies, risk y audit fuerte. Nexus no debe importar runtime LLM, memoria IA ni
agents.

## Productos

Pymes, Ponti u otros productos exponen capabilities y manifiestos. Su lógica
vertical no debe crecer dentro de Companion. `internal/watchers` debe operar
contra capabilities genéricas; el código vertical queda encapsulado en adapters
de connector o en el producto.

## Platform

`platform/*` contiene primitivas técnicas y kernels compartidos: HTTP clients,
DB, auth, logger, errores, middlewares, contratos y clientes Nexus. En este repo
no hay carpeta local `platform`; se consume como dependency externa.

Los componentes UI compartidos se consumen como paquetes `platform-*`. Si se
agregan componentes nuevos, deben ser reutilizables y sin dominio pesado.

La UI operativa para Nexus y Companion pertenece a `../console` y debe
comunicarse por `../bff`/HTTP con identidad delegada.
