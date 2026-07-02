# Security

## Auth

Companion requiere API key u OIDC/JWT interno. El middleware valida el
principal recibido desde servicios/gateways confiables y construye el
`IdentityContext` canonico:

- `customer_org_id` desde claim `org_id`.
- `human_user_id` desde `actor_id` cuando el actor es humano.
- `actor_type`, `product_surface`, `service_principal` y `on_behalf_of`.
- `companion_principal`, default `companion.employee_ai`.
- `scopes` desde `scope`, `scp` o metadata autenticada.

JWT interno service-to-service debe incluir `iss`, `aud`, `exp`, `iat` y `kid`
cuando la key sea rotativa. `aud` debe apuntar al servicio Axis receptor,
`exp` debe ser corto, y `kid` permite publicar una key nueva manteniendo una
key anterior durante el periodo de gracia. La politica inicial de rotacion se
documenta en `product-integration-contract-v1.md`; mientras no haya JWKS
productivo, dev puede usar keys estaticas acotadas por entorno.

API keys soportan metadata inline: `actor`, `org_id`, `scopes` y
`service_principal`.

Headers `X-Org-ID`, `X-User-ID` y `X-Auth-Scopes` se mantienen como
compatibilidad temporal para dev/tests cuando no hay principal autenticado; no
son la fuente canonica en runtime productivo.

Companion no expone una UI propia ni espera sesiones de browser directas. Las
consolas operativas deben autenticar usuarios fuera de Companion y llamarlo con
identidad delegada.

## Product JWT (per-producto)

Ademas del JWT interno de plataforma, Companion y Nexus aceptan JWTs HS256
emitidos por productos conectados. Cada producto tiene su propio secret e
issuer, configurados por env:

- `COMPANION_PRODUCT_JWT_KEYS` (Companion)
- `NEXUS_PRODUCT_JWT_KEYS` (Nexus)

Formato (entries separados por `;`, `,` o newline; atributos con `|`):

```
ponti=local-dev-ponti-jwt-secret|issuer=ponti-core
```

Contrato de claims del token emitido por el producto:

- `iss`: issuer del producto (debe coincidir con el configurado).
- `aud`: audiencia del servicio Axis receptor (`companion` o `nexus`).
- `sub` / `actor_id`: identidad del caller.
- `org_id`: customer org de Axis.
- `product_surface`: producto emisor.
- `scope`/`scopes`: scopes Axis solicitados.
- `service_principal`: `true` para tokens service-to-service.
- `on_behalf_of`: usuario delegado cuando aplica.
- `exp` corto obligatorio (`iat` recomendado).

Los principals autenticados por esta via quedan con
`AuthMethod=product_jwt`, distinguible de `api_key` y de `internal_jwt`.
Regla de seguridad: la delegacion de `decided_by` en approvals de Nexus
exige `AuthMethod=api_key`; un product JWT con `service_principal:true` NO
puede delegar via `X-On-Behalf-Of` ni `body.decided_by` (guard de regresion
en `nexus/internal/approvals/handler_test.go`). Si un producto necesita
delegacion legitima por JWT, requiere un diseno explicito, no un bypass.

## Scopes

Endpoints sensibles usan scopes: tasks, watchers y
nexus-assist. El API key admin de dev incluye todos los scopes necesarios.

## Customer org isolation

- `org_id` representa la customer org donde trabaja Companion, no ownership
  administrativo del runtime global.
- Tasks listadas por customer org ya no incluyen tasks con `org_id` vacío.
- Un principal con `org_id` no puede acceder tasks con `org_id` vacío.
- Watcher alerts preservan `OrgID`.
- Memory valida scope contra usuario/org/task.
- Runtime tools requieren customer org/user/scopes antes de exponerse al LLM.
- Cross-org directo en Companion requiere `companion:cross_org`; el BFF puede
  seleccionar org con `X-Axis-Org-ID` y enviar un JWT interno ya acotado.

## Prompt injection

El runtime rechaza patrones básicos de prompt injection en mensajes y args de
tools. Esto es una guardrail mínima, no una política de seguridad completa.

## Adversarial evals

La suite `scripts/evals/security-adversarial.json` cubre regresiones de:

- prompt injection directa e indirecta;
- exfiltración de datos;
- SSRF contra metadata/local file targets;
- bypass de approval/Nexus;
- poisoning de memoria;
- leakage de secretos.

Runner:

```bash
bash scripts/evals/run-security-evals.sh
```

`make check-companion` ejecuta esta suite. Los umbrales actuales fallan cerrado:
un finding esperado no detectado o un guardrail esperado no disparado deja el
check rojo.

## Secret handling

Evidence operativa sanitiza claves sensibles conocidas. No se
deben registrar API keys, bearer tokens ni payloads sensibles sin redacción.

Product installations no guardan secretos planos. `api_key_ref`, `oauth2` y
`custom` requieren `secret_ref` con esquema valido. El adapter local/dev resuelve
solo `env:`; `vault:` y `secretmanager:` quedan como contrato para adapters
productivos. Los valores resueltos se encapsulan como secretos redactados: APIs,
logs y observability no deben serializar el valor.
