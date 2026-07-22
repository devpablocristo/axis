# MCP tool governance

Companion v2 exposes an authenticated JSON-RPC 2.0 MCP facade at `POST /mcp`
and `POST /v1/mcp`. The initial surface supports `initialize`, `tools/list`, and
`tools/call`; resources, prompts, streaming, and external MCP servers are out of
scope.

Capabilities are the only tool catalog. A capability is advertised only when
it is active, its promoted manifest is conformant, it is assigned to the active
Virployee, its autonomy and professional authority allow it for the selected
work subject/case, the effective tenant/product/Job Role MCP policy permits it,
and a local governed executor is registered. Every request uses tenant and actor
identity supplied by trusted middleware and resolves an active continuity
assignment; caller arguments cannot override either identity.

The tenant policy is disabled by default. Denylists and global or per-capability
kill switches take precedence over allowlists. Owners and admins manage the
versioned policy through `GET/PUT /v1/runtime/mcp-policy` and inspect its change
history and metadata-only invocation audit through the corresponding audit APIs.

`ToolInvocationGate` is shared by the MCP facade and the internal execution
path. Inputs and outputs are validated against the promoted manifest schemas.
Reads require a registered executor. Writes require a stable idempotency key and
pass through Execution Gate and Nexus; approval binds tenant, actor, Virployee,
subject/case, continuity assignment revision, delegation/authority snapshot,
capability manifest and its product surface, MCP policy revision, active Nexus
policy snapshot, payload hash, and idempotency hash. Nexus receives only safe
metadata and opaque internal subject/case references. Those mutable inputs are
revalidated immediately before execution, so a policy, assignment, delegation,
functional-authority or manifest change invalidates an earlier approval.

The database has a uniqueness barrier for write idempotency. Concurrent retries
with the same key and identical payload/context reuse the prior pending approval;
reuse with different payload or authority context fails closed. Invocation audit
stores identifiers, statuses, and hashes only. Raw tool arguments, results,
documents, conversations, and patient display data are not persisted or sent to
Nexus.
