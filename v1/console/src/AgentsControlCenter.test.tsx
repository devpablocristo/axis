import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AgentsControlCenter } from './AgentsControlCenter'
import { createVirployee, updateVirployee, upsertJobRole } from './api'

const crudPageProps = vi.hoisted(() => [] as Array<Record<string, unknown>>)

vi.mock('@devpablocristo/platform-crud-ui', () => ({
  crudStringsEs: {},
  CrudPage: (props: Record<string, unknown>) => {
    crudPageProps.push(props)
    const slot = props.listHeaderInlineSlot as (() => ReactNode) | undefined
    const columns = (props.columns ?? []) as Array<{ header?: string }>
    return (
      <div data-testid="crud-page">
        <h2>{String(props.labelPluralCap ?? '')}</h2>
        <p>{String(props.basePath ?? '')}</p>
        <p>{String(props.searchPlaceholder ?? '')}</p>
        <p>{String(props.emptyState ?? '')}</p>
        <div>
          {columns.map((column, index) => <span key={`${column.header ?? 'column'}-${index}`}>{column.header ?? ''}</span>)}
        </div>
        {slot?.()}
      </div>
    )
  },
}))

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api')
  return {
    ...actual,
    archiveVirployeeProfile: vi.fn(),
    axisCrudHttpClient: vi.fn(() => ({ json: vi.fn() })),
    createVirployeeProfile: vi.fn(),
    createHandoff: vi.fn(),
    createVirployee: vi.fn(),
    listVirployeeProfiles: vi.fn(async () => [{
      profile_id: '11111111-1111-4111-8111-111111111111',
      profile_key: 'support.v1',
      family_id: 'support',
      version_label: 'v1',
      name: 'Support',
      max_autonomy: 'A2',
      enabled: true,
    }]),
    listHandoffs: vi.fn(async () => []),
    listIAMTenants: vi.fn(async () => [{ id: 'org-a', name: 'Org A', status: 'active' }]),
    listIAMUsers: vi.fn(async () => [{
      id: 'tenant__tenant-a__user-a',
      user_id: 'user-a',
      email: 'admin@org-a.local',
      role: 'admin',
      org_id: 'org-a',
      tenant_id: 'tenant-a',
      scope: 'tenant',
      status: 'active',
    }]),
    listJobRoles: vi.fn(async () => [{
      job_role_id: '22222222-2222-4222-8222-222222222222',
      org_id: 'org-a',
      product_surface: 'axis',
      name: 'Billing Specialist',
      slug: 'billing-specialist',
      mission: 'Keep billing clean',
      responsibilities: [{ title: 'Review invoices' }],
      recommended_capability_ids: ['33333333-3333-4333-8333-333333333333'],
      recommended_capabilities: ['billing.read'],
      default_autonomy_level: 'A2',
      status: 'active',
    }]),
    listVirployees: vi.fn(async () => []),
    purgeVirployeeProfile: vi.fn(),
    archiveJobRole: vi.fn(),
    restoreJobRole: vi.fn(),
    restoreVirployeeProfile: vi.fn(),
    trashVirployeeProfile: vi.fn(),
    trashJobRole: vi.fn(),
    updateVirployeeProfile: vi.fn(),
    updateHandoff: vi.fn(),
    updateVirployee: vi.fn(),
    upsertJobRole: vi.fn(),
  }
})

describe('AgentsControlCenter as Virployees surface', () => {
  it('uses the Virployees endpoint and public labels', async () => {
    crudPageProps.length = 0

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" productSurface="medmory" />)

    expect(await screen.findByRole('button', { name: 'Virployees' })).toBeInTheDocument()
    await waitFor(() => {
      expect(crudPageProps.at(-1)?.dataSource).toBeTruthy()
    })
    expect(screen.getAllByText('Virployees').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Buscar virployees')).toBeInTheDocument()
    expect(screen.getByText('Sin virployees')).toBeInTheDocument()
    expect(screen.getByText('Tenant')).toBeInTheDocument()
    expect(screen.queryByText('Org')).not.toBeInTheDocument()
    expect(screen.queryByText('Contexto')).not.toBeInTheDocument()
  })

  it('creates Virployees with the clean domain payload', async () => {
    crudPageProps.length = 0
    vi.mocked(createVirployee).mockClear()

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" productSurface="medmory" />)

    await waitFor(() => {
      expect(crudPageProps.at(-1)?.dataSource).toBeTruthy()
    })
    const props = crudPageProps.at(-1) as {
      formFields: Array<{ key: string; label: string; type?: string; options?: Array<{ label: string; value: string }>; fullWidth?: boolean }>
      dataSource: {
        create: (values: Record<string, string | boolean>) => Promise<void>
      }
      supportsTrash?: boolean
      trashEmptyState?: string
      toolbarActions: Array<{ label: string }>
    }
    expect(props.formFields.map((field) => field.label)).toEqual(expect.arrayContaining([
      'Supervisor',
      'Job Role',
      'Perfil',
      'Autonomía',
      'Capability IDs',
      'Memory ID',
    ]))
    expect(props.formFields.find((field) => field.key === 'supervisor_user_id')).toMatchObject({
      label: 'Supervisor',
      type: 'select',
      fullWidth: true,
      options: [{ label: 'admin@org-a.local · Admin', value: 'user-a' }],
    })
    expect(props.formFields.map((field) => field.label)).not.toEqual(expect.arrayContaining([
      'Tools',
      'Puesto / Job title',
      'Misión',
      'Metadata JSON',
    ]))

    await props.dataSource.create({
      name: 'Finance Employee',
      supervisor_user_id: '44444444-4444-4444-8444-444444444444',
      profile_id: '11111111-1111-4111-8111-111111111111',
      job_role_id: '22222222-2222-4222-8222-222222222222',
      autonomy: 'A2',
      capability_ids: '55555555-5555-4555-8555-555555555555',
      memory_id: '',
    })

    expect(createVirployee).toHaveBeenCalledWith(
      'org-a',
      {
        name: 'Finance Employee',
        supervisor_user_id: '44444444-4444-4444-8444-444444444444',
        job_role_id: '22222222-2222-4222-8222-222222222222',
        profile_id: '11111111-1111-4111-8111-111111111111',
        autonomy: 'A2',
        capability_ids: ['55555555-5555-4555-8555-555555555555'],
        memory_id: null,
      },
      'tenant-a',
    )
  })

  it('does not apply Job Role defaults when editing an existing Virployee', async () => {
    crudPageProps.length = 0
    vi.mocked(createVirployee).mockClear()
    vi.mocked(updateVirployee).mockClear()

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" productSurface="medmory" />)

    await waitFor(() => {
      const props = crudPageProps.at(-1) as {
        dataSource?: {
          create: (values: Record<string, string | boolean>) => Promise<void>
        }
      }
      expect(props.dataSource).toBeTruthy()
    })

    const props = crudPageProps.at(-1) as {
      dataSource: {
        create: (values: Record<string, string | boolean>) => Promise<void>
        update: (row: { id: string }, values: Record<string, string | boolean>) => Promise<void>
      }
    }
    await props.dataSource.create({
      name: 'Billing Employee',
      supervisor_user_id: '44444444-4444-4444-8444-444444444444',
      profile_id: '11111111-1111-4111-8111-111111111111',
      autonomy: 'A2',
      job_role_id: '22222222-2222-4222-8222-222222222222',
      capability_ids: '',
      memory_id: '',
    })
    expect(createVirployee).toHaveBeenCalledWith(
      'org-a',
      expect.objectContaining({
        capability_ids: ['33333333-3333-4333-8333-333333333333'],
      }),
      'tenant-a',
    )

    await props.dataSource.update({ id: 'employee-1' }, {
        name: 'Billing Employee',
        supervisor_user_id: '44444444-4444-4444-8444-444444444444',
        profile_id: '11111111-1111-4111-8111-111111111111',
        autonomy: 'A2',
        job_role_id: '22222222-2222-4222-8222-222222222222',
        capability_ids: '',
        memory_id: '',
      })
    expect(updateVirployee).toHaveBeenCalledWith(
      'org-a',
      'employee-1',
      expect.objectContaining({
        capability_ids: [],
      }),
      'tenant-a',
    )
  })

  it('keeps Job Role creation human-facing and generates technical fields', async () => {
    crudPageProps.length = 0
    vi.mocked(upsertJobRole).mockClear()

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" productSurface="medmory" />)

    fireEvent.click(await screen.findByRole('button', { name: 'Job Roles' }))

    await waitFor(() => {
      expect(crudPageProps.at(-1)?.labelPluralCap).toBe('Job Roles')
    })
    const props = crudPageProps.at(-1) as {
      formFields: Array<{ key: string; label: string }>
      supportsTrash?: boolean
      trashEmptyState?: string
      toolbarActions: Array<{ label: string }>
      dataSource: {
        create: (values: Record<string, string | boolean>) => Promise<void>
      }
    }
    const labels = props.formFields.map((field) => field.label)

    expect(labels).toEqual([
      'Nombre',
      'Misión',
      'Responsabilidades',
      'Capabilities recomendadas',
      'Autonomía default',
      'Criterios de éxito',
    ])
    expect(labels.some((label) => label.includes('JSON'))).toBe(false)
    expect(labels).not.toContain('Job Role ID')
    expect(labels).not.toContain('Slug')
    expect(props.supportsTrash).toBe(true)
    expect(props.trashEmptyState).toBe('Sin job roles en papelera')
    expect(props.toolbarActions.map((action) => action.label)).toEqual(['Activos', 'Archivados', 'Papelera'])

    await props.dataSource.create({
      name: 'Medical Case Assistant',
      mission: 'Support medical review without autonomous diagnosis',
      responsibilities: 'Resumir historia clínica\nDetectar señales de alarma, inconsistencias y datos faltantes',
      recommended_capabilities: 'medical.records.read, medical.summary.generate',
      default_autonomy_level: 'A1',
      success_criteria: 'evidencia preparada\nrevisión humana requerida',
    })

    expect(upsertJobRole).toHaveBeenCalledWith(
      'org-a',
      'medical-case-assistant',
      expect.objectContaining({
        name: 'Medical Case Assistant',
        slug: 'medical-case-assistant',
        responsibilities: [
          { title: 'Resumir historia clínica', description: '', expected_outcome: '', priority: 1 },
          { title: 'Detectar señales de alarma, inconsistencias y datos faltantes', description: '', expected_outcome: '', priority: 2 },
        ],
        recommended_capabilities: ['medical.records.read', 'medical.summary.generate'],
        default_autonomy_level: 'A1',
        success_criteria: ['evidencia preparada', 'revisión humana requerida'],
        default_sla_policy: {},
        default_memory_policy: {},
        metadata: {},
      }),
      'tenant-a',
    )
    expect(vi.mocked(upsertJobRole).mock.calls.at(-1)?.[2]).not.toHaveProperty('description')
  })

  it('shows public Employee handoffs without agent fields', async () => {
    crudPageProps.length = 0

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" productSurface="medmory" />)

    fireEvent.click(await screen.findByRole('button', { name: 'Handoffs' }))

    await waitFor(() => {
      expect(crudPageProps.at(-1)?.labelPluralCap).toBe('Handoffs')
    })
    const props = crudPageProps.at(-1) as {
      formFields: Array<{ key: string; label: string }>
      columns: Array<{ header?: string }>
      toolbarActions: Array<{ label: string }>
      listHeaderInlineSlot?: () => ReactNode
      supportsArchived?: boolean
    }
    expect(props.formFields.map((field) => field.key)).toEqual([
      'task_id',
      'from_virployee_id',
      'to_virployee_id',
      'reason',
      'status',
    ])
    expect(props.formFields.map((field) => field.key)).not.toContain('from_agent_id')
    expect(props.formFields.map((field) => field.key)).not.toContain('to_agent_id')
    expect(props.columns.map((column) => column.header)).toEqual(expect.arrayContaining(['Desde', 'Hacia']))
    expect(props.supportsArchived).toBeFalsy()
    expect(props.listHeaderInlineSlot?.()).toBeTruthy()
    expect(props.toolbarActions.map((action) => action.label)).toEqual(['Todos', 'Pendientes', 'Aceptados', 'Rechazados', 'Cancelados'])
  })
})
