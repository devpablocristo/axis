# Integración de un consumidor con Axis Assist

## P1: búsqueda y timeline clínicas

Un producto externo puede registrar y usar estas capacidades de Virployees. El
producto conserva la propiedad de su configuración; Axis no conoce su nombre ni
incluye aliases, credenciales, organizations o defaults específicos del consumidor:

| Capacidad canónica | Autonomía | Timeout | Cuotas | Job Roles iniciales |
| --- | --- | --- | --- | --- |
| `clinical.records.search` | A0 | 30 s | inbound, embeddings | Medical Historian |
| `clinical.timeline.build` | A1 | 120 s | inbound, LLM | Medical Historian, Study Analyst, Care Coordinator |

Ambas son `read`, riesgo `medium`, requieren evidencia, tienen un intento y no
usan aprobación Nexus ni rollback. Ningún Virployee administrativo o de billing
debe recibirlas. El bootstrap organization-scoped crea cuotas, manifests, conformance,
promoción, MCP policy y asignaciones sin tocar otros Job Roles:

```bash
cd v2/companion
AXIS_COMPANION_URL=http://127.0.0.1:18081/v1 \
AXIS_INTERNAL_AUTH_SECRET="$COMPANION_V2_INTERNAL_AUTH_SECRET" \
AXIS_ORG_ID=<organization-uuid> \
AXIS_PRODUCT_SURFACE=<product-surface> \
./scripts/onboard-clinical-capabilities.sh
```

El `POST /v1/assist-runs` agrega `capability_key`. Para estas dos claves son
obligatorios un `subject_id` UUID, `repository_generation` estable e
`Idempotency-Key`; `case_id` es opcional. Ejemplo de búsqueda:

```bash
curl -sS -X POST http://127.0.0.1:19080/v1/assist-runs \
  -H "X-API-Key: $AXIS_API_KEY" -H "Idempotency-Key: patient-search-g42-q1" \
  -H "Content-Type: application/json" \
  -d '{"product_surface":"<product-surface>","capability_key":"clinical.records.search",
       "subject_id":"<subject-uuid>","repository_generation":"g42",
       "input":{"query":"cambios recientes de hemoglobina","limit":20}}'
```

Ejemplo de timeline regenerable:

```json
{
  "product_surface": "<product-surface>",
  "capability_key": "clinical.timeline.build",
  "subject_id": "<subject-uuid>",
  "repository_generation": "g42",
  "input": {"date_from":"2025-01-01T00:00:00Z","order":"desc","max_events":100}
}
```

La respuesta/polling incluye la clave canónica, `capability_manifest_hash`,
`answer_status`, `citations` y `output` estructurado. Timeline se reconstruye
bajo demanda y sólo puede cachearse por generación. Si el corpus supera 200
partes o 100.000 caracteres devuelve `partial`; si una reparación no elimina
afirmaciones o citas inválidas, devuelve `abstained` sin eventos.

## Assist diagnóstico existente

El consumidor (una máquina, no un usuario humano) le manda a Axis un JSON
con **referencias a documentos** y recibe un **panorama diagnóstico**. El arco:

```
consumer → POST {AXIS_COMPANION_BASE_URL}/v1/assist-runs  [X-API-Key]
        → BFF inbound: la key → organization + product surface + virployee + service principal
        → companion POST /v1/virployees/{id}/assist  (X-Axis-Internal-Token + X-Org-ID + X-Actor-ID)
        → companion: reserva caso+corrida (idempotentes) → staging/índice del corpus
                     → policy selector: direct | consult | needs_human
                     → especialistas acotados (si aplica) → síntesis del único responsable
        → BFF mapea a { id, case_id, responsible_virployee_id, status, orchestration, output }
```

Modelo **pull**: el consumidor manda `read_url` presignadas; Axis lee los documentos. Sin
efectos externos ni aprobación (read/explain). El `output` es el informe del médico
(author, summary, conditions con evidencia, recommended_next_steps, urgent_flags,
information_needed, disclaimer).

## Config (local)

- **companion:** `COMPANION_V2_RUNTIME_BASE_URL=http://runtime-v2:8080` (ya en el compose;
  sin esto el answerer queda nil y el assist devuelve "runtime answerer is not configured").
- **BFF:** `BFF_V2_PRODUCT_API_KEYS` con la credencial del consumidor. El quinto
  campo (`routing_pool_id`) es opcional; cuando está presente el BFF resuelve la
  asignación estable antes de enviar Assist. Sin él se conserva el Virployee
  fijo sólo para integraciones legacy fuera de pools. Formato
  `<apiKey>=<organization>|<virployee>|<actor>|<product>`. Ejemplo neutro:
  ```
  BFF_V2_PRODUCT_API_KEYS=local-key=8c3a623a-f9d2-44d2-a71e-e9c14992031e|3e5a24e1-cfe2-44c9-8c15-698de5dade5a|service:external-product|external-product|POOL_UUID
  ```
  Ponelo en `.env` o `docker-compose.override.yml` (no commitear la key real).
- El consumidor debe crear y asignar el **Virployee** apropiado dentro de su
  organization. El LLM real requiere credenciales del runtime (Vertex/ADC); sin
  ellas corre en **Echo** y la corrida sale `degraded` (sin diagnóstico real, pero el arco
  funciona).

## Levantar y probar

```bash
cd v2 && make up
# smoke (AXIS_API_KEY = la key que configuraste en BFF_V2_PRODUCT_API_KEYS):
curl -sS -X POST http://127.0.0.1:19080/v1/assist-runs \
  -H "X-API-Key: $AXIS_API_KEY" -H "Idempotency-Key: diagnosis-g42-v1" \
  -H "Content-Type: application/json" \
  -d '{"owner_system":"external-product","product_surface":"external-product","assist_type":"clinical_diagnosis",
       "subject_type":"repository","subject_id":"<subject-uuid>","repository_generation":"g42",
       "input":{"schema_version":"external.diagnosis_input.v1",
                "documents":[{"key":"labs.txt","read_url":"<url-presignada>","content_type":"text/plain"}]}}'
```

Esperado: `200` con `{ id, status:"completed", output, error_message:"" }`. Con Echo,
`output` viene vacío/degradado (el arco anduvo pero el LLM no está configurado). Confirmá
la corrida: `SELECT status, answered, degraded FROM companion_assist_runs ORDER BY started_at DESC LIMIT 1;`
Repetir con la misma `Idempotency-Key` devuelve la misma corrida sin re-invocar el modelo.
Si el trabajo continúa, la respuesta es `202` con `status_url`; el consumidor consulta
`GET /v1/assist-runs/:id`. `needs_human` es terminal y devuelve `200` con el caso y
la trazabilidad de revisión, no `503`.

## Cambiar al entorno real de Axis

En el consumidor, apuntá `AXIS_COMPANION_BASE_URL` al Axis real y usá la
`AXIS_COMPANION_API_KEY` configurada. El contrato no cambia por producto.

## Activar especialistas

La política se configura desde `Coordination` en la Console o por la superficie
humana `/api`. Primero deben existir y estar activas las capabilities de selector,
consulta y síntesis, y cada capability de consulta debe estar asignada al
Virployee especialista.

1. Crear rutas con códigos estables y namespaced, por ejemplo
   `clinical.cardiology` o `clinical.laboratory`; el selector sólo ve esos códigos.
2. Crear la policy de `<product-surface> / clinical_diagnosis` en modo `shadow`. Validar
   decisiones, cuotas, latencia, ledger y que ningún selector invente destinos.
3. Pasar a `active` cuando el schema de salida y los especialistas estén
   conformantes. El límite es tres especialistas y profundidad uno.
4. Supervisar casos, handoffs y revisiones humanas en `Coordination`. Sólo un
   supervisor/admin/owner humano puede transferir la responsabilidad; el service
   principal del consumidor no puede aprobar ni aceptar su propio handoff.

Las opiniones especializadas nunca son la respuesta pública: el dueño vigente
produce una única síntesis, incluyendo discrepancias y limitaciones. Si una
consulta requerida falla, no se emite diagnóstico parcial; una consulta advisory
fallida queda declarada como limitación.

## Soporte documental y límites

Axis conserva el original verificado y procesa texto, PDF nativo/escaneado,
Office, imágenes, audio, video y DICOM mediante adapters aislados; los derivados
(texto, OCR, tablas, captions, transcript, keyframes) no reemplazan el original.
El corpus se stagea e indexa una vez por generación y todos los especialistas
reutilizan ese mismo scope organization/virployee/producto/subject. Un formato no
soportado o un documento requerido ilegible falla explícitamente: nunca se
convierte un binario en texto vacío.

Los límites publicados por `GET /v1/assist-capabilities` son la autoridad del
cliente. La configuración actual impone 250 MiB por artefacto, 500 MiB por
diagnóstico y 5 GiB por repositorio; las cuotas del organization/producto pueden ser
más restrictivas y devuelven `429` con `Retry-After`.
