# Memory

La memoria actual vive en `companion_memory_entries`.

## Modelo actual

- `kind`: `task_summary`, `task_facts`, `playbook_snippet`,
  `user_preference`, `episodic_event`, `semantic_fact`,
  `operational_state`.
- `memory_type`: `episodic`, `semantic`, `operational`, `preference`,
  `playbook` o `task_projection`.
- `org_id`: tenant propietario de la memoria. Es obligatorio en writes/reads
  operativos.
- `user_id`: usuario propietario cuando aplica (`scope_type=user` o
  provenance de una task creada por usuario).
- `product_surface`: superficie/producto que originó y puede recuperar la
  memoria. Default: `companion`.
- `classification`: `stable`, `operational` o `audit`.
- `scope_type`: `task`, `org`, `user`.
- `scope_id`: ID del scope.
- `content_text` y `payload_json`.
- `provenance_json`, `confidence` y `retention_policy`.
- `version` para optimistic locking.
- `expires_at` para olvido por TTL.

## Reglas de aislamiento

- Scope `org`: `scope_id` debe coincidir con `X-Org-ID`.
- Scope `user`: `scope_id` debe ser `X-Org-ID:X-User-ID`.
- Scope `task`: se resuelve el `org_id` de la task y debe coincidir con el
  principal.
- `product_surface` de la entrada debe coincidir con `X-Product-Surface`
  (`companion` si el header no viene).
- Runtime `remember`/`recall` no usa `"default"`; sin identidad válida
  responde error y no persiste memoria compartida.

## Tipos

La separación final vive en `memory_type`. `classification` queda para
estabilidad/sensibilidad operativa; no debe usarse como tipo de memoria.

## Evolución

El siguiente cambio estructural debe convertir `retention_policy` en reglas
ejecutables de recuperación/olvido y validar constraints ya cargadas como
`NOT VALID`.
