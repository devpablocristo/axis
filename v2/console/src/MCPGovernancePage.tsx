import { useCallback, useEffect, useState } from 'react'
import {
	getMCPPolicy,
	listMCPInvocations,
	listMCPPolicyAudit,
	putMCPPolicy,
	type MCPInvocationAudit,
	type MCPPolicy,
	type MCPPolicyAudit,
} from './api'
import { formatDateTime24 } from './formatters'

export function MCPGovernancePage({ orgId, principalId }: { orgId: string; principalId: string }) {
	const [policy, setPolicy] = useState<MCPPolicy | null>(null)
	const [invocations, setInvocations] = useState<MCPInvocationAudit[]>([])
	const [policyAudit, setPolicyAudit] = useState<MCPPolicyAudit[]>([])
	const [loading, setLoading] = useState(true)
	const [saving, setSaving] = useState(false)
	const [error, setError] = useState('')
	const [switchesText, setSwitchesText] = useState('')

	const load = useCallback(async () => {
		if (!orgId) return
		setLoading(true)
		setError('')
		try {
			const [nextPolicy, nextInvocations, nextPolicyAudit] = await Promise.all([
				getMCPPolicy(orgId, principalId),
				listMCPInvocations(orgId, principalId),
				listMCPPolicyAudit(orgId, principalId),
			])
			setPolicy(nextPolicy)
			setInvocations(nextInvocations)
			setPolicyAudit(nextPolicyAudit)
			setSwitchesText(Object.entries(nextPolicy.capability_kill_switches).filter(([, enabled]) => enabled).map(([key]) => key).sort().join('\n'))
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : 'Could not load MCP governance')
		} finally {
			setLoading(false)
		}
	}, [principalId, orgId])

	useEffect(() => { void load() }, [load])

	const save = async () => {
		if (!policy || saving) return
		setSaving(true)
		setError('')
		try {
			const killSwitches = Object.fromEntries(splitLines(switchesText).map((key) => [key, true]))
			const saved = await putMCPPolicy({ ...policy, capability_kill_switches: killSwitches }, orgId, principalId)
			setPolicy(saved)
			setSwitchesText(Object.keys(killSwitches).sort().join('\n'))
			setInvocations(await listMCPInvocations(orgId, principalId))
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : 'Could not save MCP governance')
		} finally {
			setSaving(false)
		}
	}

	if (loading && !policy) return <div className="spinner" />
	if (!policy) return <section className="page-section"><p role="alert" className="iam-control__inline-error">{error || 'MCP policy is unavailable'}</p></section>

	return (
		<section className="page-section">
			<div className="card crud-form-card">
				<div className="card-header"><div><h2>MCP governance</h2><p className="axis-muted">Capabilities remain the source of truth. Denials and kill switches always win.</p></div></div>
				{error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}
				<div className="virployee-preview__grid">
					<label className="form-group"><span>Enabled</span><input type="checkbox" checked={policy.enabled} onChange={(event) => setPolicy({ ...policy, enabled: event.currentTarget.checked })} /></label>
					<label className="form-group"><span>Global kill switch</span><input type="checkbox" checked={policy.kill_switch} onChange={(event) => setPolicy({ ...policy, kill_switch: event.currentTarget.checked })} /></label>
					<label className="form-group"><span>Maximum risk</span><select value={policy.max_risk_class} onChange={(event) => setPolicy({ ...policy, max_risk_class: event.currentTarget.value as MCPPolicy['max_risk_class'] })}><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option><option value="critical">Critical</option></select></label>
					<label className="form-group"><span>Calls per minute</span><input type="number" min={1} value={policy.max_calls_per_minute} onChange={(event) => setPolicy({ ...policy, max_calls_per_minute: Number(event.currentTarget.value) })} /></label>
					<label className="form-group"><span>Concurrent calls</span><input type="number" min={1} value={policy.max_concurrency} onChange={(event) => setPolicy({ ...policy, max_concurrency: Number(event.currentTarget.value) })} /></label>
					<label className="form-group"><span>Policy revision</span><input value={String(policy.version)} disabled /></label>
				</div>
				<div className="virployee-preview__grid">
					<label className="form-group"><span>Allowed capabilities</span><textarea rows={5} value={policy.allowed_capabilities.join('\n')} onChange={(event) => setPolicy({ ...policy, allowed_capabilities: splitLines(event.currentTarget.value) })} placeholder="Empty allows any otherwise-authorized capability" /></label>
					<label className="form-group"><span>Denied capabilities</span><textarea rows={5} value={policy.denied_capabilities.join('\n')} onChange={(event) => setPolicy({ ...policy, denied_capabilities: splitLines(event.currentTarget.value) })} placeholder="calendar.events.delete" /></label>
					<label className="form-group"><span>Capability kill switches</span><textarea rows={5} value={switchesText} onChange={(event) => setSwitchesText(event.currentTarget.value)} placeholder="calendar.events.create" /></label>
				</div>
				<footer className="virployee-panel-footer"><button type="button" className="btn-primary" disabled={saving} onClick={() => void save()}>{saving ? 'Saving…' : 'Save MCP policy'}</button><button type="button" className="btn-secondary" onClick={() => void load()}>Refresh</button></footer>
			</div>

			<div className="card">
				<div className="card-header"><div><h2>Policy changes</h2><p className="axis-muted">Organization policy revision history and responsible actor.</p></div></div>
				<div className="table-wrap"><table><thead><tr><th>Time</th><th>Actor</th><th>Previous</th><th>New</th></tr></thead><tbody>
					{policyAudit.length === 0 ? <tr><td colSpan={4}>No policy changes</td></tr> : policyAudit.map((item) => <tr key={item.id}><td>{formatDateTime24(item.created_at)}</td><td>{item.actor_id}</td><td>v{item.previous_version}</td><td>v{item.new_version}</td></tr>)}
				</tbody></table></div>
			</div>

			<div className="card">
				<div className="card-header"><div><h2>Invocation audit</h2><p className="axis-muted">Metadata and hashes only; arguments, results and patient content are not stored.</p></div></div>
				<div className="table-wrap"><table><thead><tr><th>Time</th><th>Virployee</th><th>Method</th><th>Capability</th><th>Status</th><th>Blocked by</th><th>Duration</th></tr></thead><tbody>
					{invocations.length === 0 ? <tr><td colSpan={7}>No MCP invocations</td></tr> : invocations.map((item) => <tr key={item.id}><td>{formatDateTime24(item.created_at)}</td><td>{item.context.virployee_id}</td><td>{item.method}</td><td>{item.capability_key || '-'}</td><td>{item.status}</td><td>{item.blocked_by || '-'}</td><td>{item.duration_ms} ms</td></tr>)}
				</tbody></table></div>
			</div>
		</section>
	)
}

function splitLines(value: string): string[] {
	return Array.from(new Set(value.split(/\r?\n|,/).map((item) => item.trim().toLowerCase()).filter(Boolean))).sort()
}
