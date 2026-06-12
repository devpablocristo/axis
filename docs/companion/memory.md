# Memory

La memoria actual vive en `companion_memory_entries`.

## Modelo actual

- `kind`: `task_summary`, `task_facts`, `playbook_snippet`,
  `user_preference`, `episodic_event`, `semantic_fact`,
  `operational_state`.
- `memory_type`: `episodic`, `semantic`, `operational`, `preference`,
  `playbook` o `task_projection`.
- `org_id`: customer org propietaria de la memoria. Es obligatorio en
  writes/reads operativos.
- `user_id`: usuario propietario cuando aplica (`scope_type=user` o
  provenance de una task creada por usuario).
- `product_surface`: superficie/producto que originó y puede recuperar la
  memoria. Default: `companion`.
- `classification`: `stable`, `operational` o `audit`.
- `scope_type`: `task`, `org`, `user`.
- `scope_id`: ID del scope.
- `content_text` y `payload_json`.
- `provenance_json`, `confidence` y `retention_policy`.
- `trust_score`, `status`, `source`, `poisoning_flags`,
  `supersedes_id/superseded_by_id`, `conflict_group_id`,
  `last_verified_at`, `confidence_decay_at`.
- `embedding_namespace`, `embedding_model` y `embedding_json` para búsqueda
  rankeada aislada por customer org/product surface.
- `version` para optimistic locking.
- `expires_at` para olvido por TTL.

## Reglas de aislamiento

- Scope `org`: `scope_id` debe coincidir con el `customer_org_id` resuelto
  desde el `IdentityContext`.
- Scope `user`: `scope_id` debe ser `customer_org_id:human_user_id`.
- Scope `task`: se resuelve el `org_id` de la task y debe coincidir con el
  principal.
- `product_surface` de la entrada debe coincidir con el claim/surface resuelto
  (`companion` si no viene).
- Runtime `remember`/`recall` no usa `"default"`; sin identidad válida
  responde error y no persiste memoria compartida.

## Tipos

La separación final vive en `memory_type`. `classification` queda para
estabilidad/sensibilidad operativa; no debe usarse como tipo de memoria.

## Memory v2

La escritura de memoria ahora:

- exige provenance por default desde el control plane;
- calcula embeddings namespaced por `org_id:product_surface[:agent_id]`;
- usa `pgvector` como búsqueda primaria cuando la extensión está disponible y
  mantiene `embedding_json` como fallback/audit;
- deja `hash-v1` solo como provider determinístico de dev/test/fallback local;
- asigna `trust_score` desde confidence y señales adversariales;
- rechaza memory poisoning salvo override explícito de infraestructura;
- detecta conflictos en semantic/tenant/business memory de alta confianza;
- permite supersession explícita;
- registra audit append-only en `companion_memory_audit`.

La búsqueda `GET /v1/memory/search` combina similitud vectorial local,
coincidencia textual, confidence decay y trust score. No recupera memorias
rechazadas/superseded ni memorias con poisoning flags salvo que el caller use
infraestructura interna que lo permita.

`companion_memory_summaries` queda como tabla versionada para compactación y
summaries curados por scope.

## Providers de embeddings

Variables operativas:

- `COMPANION_EMBEDDING_PROVIDER`: `vertex`, `vertex_ai` o `hash-v1`.
- `COMPANION_EMBEDDING_MODEL`: modelo efectivo registrado en memoria.
- `COMPANION_EMBEDDING_VERTEX_PROJECT`: proyecto GCP para Vertex embeddings.
- `COMPANION_EMBEDDING_VERTEX_LOCATION`: región de Vertex. Default:
  `us-central1`.
- `COMPANION_EMBEDDING_DIMENSIONS`: dimensión esperada por provider/vector
  store. Default local: `64`.

En producción debe configurarse provider real; si no hay provider/proyecto,
Companion cae a `hash-v1` para mantener entornos locales reproducibles.
