# medmory → Axis "assist" (diagnóstico) — runbook

medmory (un producto externo, una máquina — no un usuario) le manda a Axis un JSON
con **referencias a documentos** y recibe un **panorama diagnóstico**. El arco:

```
medmory → POST {AXIS_COMPANION_BASE_URL}/v1/assist-runs  [X-API-Key]
        → BFF inbound: la key → tenant (cristo.tech × medmory) + virployee (médico) + actor service-principal
        → companion POST /v1/virployees/{id}/assist  (X-Axis-Internal-Token + X-Tenant-ID + X-Actor-ID)
        → companion: reserva caso+corrida (idempotentes) → staging/índice del corpus
                     → policy selector: direct | consult | needs_human
                     → especialistas acotados (si aplica) → síntesis del único responsable
        → BFF mapea a { id, case_id, responsible_virployee_id, status, orchestration, output }
```

Modelo **pull**: medmory manda `read_url` presignadas; Axis lee los documentos. Sin
efectos externos ni aprobación (read/explain). El `output` es el informe del médico
(author, summary, conditions con evidencia, recommended_next_steps, urgent_flags,
information_needed, disclaimer).

## Config (local)

- **companion:** `COMPANION_V2_RUNTIME_BASE_URL=http://runtime-v2:8080` (ya en el compose;
  sin esto el answerer queda nil y el assist devuelve "runtime answerer is not configured").
- **BFF:** `BFF_V2_PRODUCT_API_KEYS` con la credencial de medmory. El quinto
  campo (`routing_pool_id`) es opcional; cuando está presente el BFF resuelve la
  asignación estable antes de enviar Assist. Sin él se conserva el Virployee
  fijo sólo para integraciones legacy fuera de pools. Formato
  `<apiKey>=<tenant>|<virployee>|<actor>|<product>`. Ejemplo (tenant cristo.tech × medmory):
  ```
  BFF_V2_PRODUCT_API_KEYS=medmory-local-key=8c3a623a-f9d2-44d2-a71e-e9c14992031e|3e5a24e1-cfe2-44c9-8c15-698de5dade5a|service:medmory|medmory|POOL_UUID
  ```
  Ponelo en `.env` o `docker-compose.override.yml` (no commitear la key real).
- El **médico** (virployee "Médico clínico (medmory)", A2, con el system prompt clínico)
  ya vive bajo ese tenant. El LLM real requiere credenciales del runtime (Vertex/ADC); sin
  ellas corre en **Echo** y la corrida sale `degraded` (sin diagnóstico real, pero el arco
  funciona).

## Levantar y probar

```bash
cd v2 && make up
# smoke (AXIS_API_KEY = la key que configuraste en BFF_V2_PRODUCT_API_KEYS):
curl -sS -X POST http://127.0.0.1:19080/v1/assist-runs \
  -H "X-API-Key: $AXIS_API_KEY" -H "Content-Type: application/json" \
  -d '{"owner_system":"medmory","product_surface":"medmory","assist_type":"clinical_diagnosis",
       "subject_type":"repository","subject_id":"default",
       "input":{"schema_version":"medmory.diagnosis_input.v1",
                "documents":[{"key":"labs.txt","read_url":"<url-presignada>","content_type":"text/plain"}]}}'
```

Esperado: `200` con `{ id, status:"completed", output, error_message:"" }`. Con Echo,
`output` viene vacío/degradado (el arco anduvo pero el LLM no está configurado). Confirmá
la corrida: `SELECT status, answered, degraded FROM companion_assist_runs ORDER BY started_at DESC LIMIT 1;`
Repetir con la misma `Idempotency-Key` devuelve la misma corrida sin re-invocar el modelo.
Si el trabajo continúa, la respuesta es `202` con `status_url`; Medmory consulta
`GET /v1/assist-runs/:id`. `needs_human` es terminal y devuelve `200` con el caso y
la trazabilidad de revisión, no `503`.

## Cambiar a Axis "de verdad" desde medmory

En medmory, apuntá `AXIS_COMPANION_BASE_URL` al Axis real y usá su `AXIS_COMPANION_API_KEY`
(= la key configurada arriba). **Cero cambios de código en medmory** — el contrato es el mismo.

## Activar especialistas para Medmory

La política se configura desde `Coordination` en la Console o por la superficie
humana `/api`. Primero deben existir y estar activas las capabilities de selector,
consulta y síntesis, y cada capability de consulta debe estar asignada al
Virployee especialista.

1. Crear rutas con códigos estables y namespaced, por ejemplo
   `clinical.cardiology` o `clinical.laboratory`; el selector sólo ve esos códigos.
2. Crear la policy de `medmory / clinical_diagnosis` en modo `shadow`. Validar
   decisiones, cuotas, latencia, ledger y que ningún selector invente destinos.
3. Pasar a `active` cuando el schema de salida y los especialistas estén
   conformantes. El límite es tres especialistas y profundidad uno.
4. Supervisar casos, handoffs y revisiones humanas en `Coordination`. Sólo un
   supervisor/admin/owner humano puede transferir la responsabilidad; el service
   principal de Medmory no puede aprobar ni aceptar su propio handoff.

Las opiniones especializadas nunca son la respuesta pública: el dueño vigente
produce una única síntesis, incluyendo discrepancias y limitaciones. Si una
consulta requerida falla, no se emite diagnóstico parcial; una consulta advisory
fallida queda declarada como limitación.

## Soporte documental y límites

Axis conserva el original verificado y procesa texto, PDF nativo/escaneado,
Office, imágenes, audio, video y DICOM mediante adapters aislados; los derivados
(texto, OCR, tablas, captions, transcript, keyframes) no reemplazan el original.
El corpus se stagea e indexa una vez por generación y todos los especialistas
reutilizan ese mismo scope tenant/virployee/producto/subject. Un formato no
soportado o un documento requerido ilegible falla explícitamente: nunca se
convierte un binario en texto vacío.

Los límites publicados por `GET /v1/assist-capabilities` son la autoridad del
cliente. La configuración actual impone 250 MiB por artefacto, 500 MiB por
diagnóstico y 5 GiB por repositorio; las cuotas del tenant/producto pueden ser
más restrictivas y devuelven `429` con `Retry-After`.
