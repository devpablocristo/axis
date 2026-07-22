# medmory → Axis "assist" (diagnóstico) — runbook

medmory (un producto externo, una máquina — no un usuario) le manda a Axis un JSON
con **referencias a documentos** y recibe un **panorama diagnóstico**. El arco:

```
medmory → POST {AXIS_COMPANION_BASE_URL}/v1/assist-runs  [X-API-Key]
        → BFF inbound: la key → tenant (cristo.tech × medmory) + virployee (médico) + actor service-principal
        → companion POST /v1/virployees/{id}/assist  (X-Axis-Internal-Token + X-Tenant-ID + X-Actor-ID)
        → companion: reserva la corrida (idempotente) → BAJA cada read_url → runtime /v1/answer
                     bajo el system prompt del médico → guarda la corrida → devuelve
        → BFF mapea a { id, status:"completed", output, error_message }
```

Modelo **pull**: medmory manda `read_url` presignadas; Axis lee los documentos. Sin
efectos externos ni aprobación (read/explain). El `output` es el informe del médico
(author, summary, conditions con evidencia, recommended_next_steps, urgent_flags,
information_needed, disclaimer).

## Config (local)

- **companion:** `COMPANION_V2_RUNTIME_BASE_URL=http://runtime-v2:8080` (ya en el compose;
  sin esto el answerer queda nil y el assist devuelve "runtime answerer is not configured").
- **BFF:** `BFF_V2_PRODUCT_API_KEYS` con la credencial de medmory. Formato
  `<apiKey>=<tenant>|<virployee>|<actor>|<product>`. Ejemplo (tenant cristo.tech × medmory):
  ```
  BFF_V2_PRODUCT_API_KEYS=medmory-local-key=8c3a623a-f9d2-44d2-a71e-e9c14992031e|3e5a24e1-cfe2-44c9-8c15-698de5dade5a|service:medmory|medmory
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

## Cambiar a Axis "de verdad" desde medmory

En medmory, apuntá `AXIS_COMPANION_BASE_URL` al Axis real y usá su `AXIS_COMPANION_API_KEY`
(= la key configurada arriba). **Cero cambios de código en medmory** — el contrato es el mismo.

## Soporte documental y límites actuales

Axis lee documentos de texto y extrae el texto embebido en archivos PDF. Los PDF escaneados
sin capa de texto y las imágenes todavía requieren soporte multimodal/OCR. La orquestación de
especialistas también queda fuera de alcance: el médico puede derivar, pero todavía no llama a
otros virployees.
