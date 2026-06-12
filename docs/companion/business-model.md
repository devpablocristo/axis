# Business Model

El modelo empresarial persistente vive por `org_id` y `product_surface`.
No hardcodea verticales: cada empresa describe su organización, procesos,
reglas y vocabulario como datos versionados.

## Contenido

El contrato soporta:

- organización, industria, locale y timezone;
- áreas, roles, usuarios y empleados IA;
- workflows, procesos y excepciones;
- reglas internas, preferencias y prioridades;
- vocabulario de negocio;
- horarios y SLAs;
- tools/capabilities relevantes;
- relaciones entre actores;
- contexto operativo libre en `context`.

## APIs

- `GET /v1/business-model`: devuelve el modelo activo de la customer org.
- `PUT /v1/business-model`: guarda una nueva versión activa y archiva la
  anterior.

Requiere `companion:runtime:admin` o `companion:cross_org`.

## Runtime

El context assembler agrega un resumen del modelo empresarial al prompt del run.
Esto permite adaptar comportamiento por negocio sin meter lógica vertical en
Companion. La fuente transaccional sigue viviendo en productos externos.
