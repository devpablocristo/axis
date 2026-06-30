import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AgentsControlCenter, VIRTUAL_EMPLOYEES_BASE_PATH } from './AgentsControlCenter'

const crudPageProps = vi.hoisted(() => [] as Array<Record<string, unknown>>)

vi.mock('@devpablocristo/platform-crud-ui', () => ({
  crudStringsEs: {},
  CrudPage: (props: Record<string, unknown>) => {
    crudPageProps.push(props)
    const slot = props.listHeaderInlineSlot as (() => ReactNode) | undefined
    return (
      <div data-testid="crud-page">
        <h2>{String(props.labelPluralCap ?? '')}</h2>
        <p>{String(props.basePath ?? '')}</p>
        <p>{String(props.searchPlaceholder ?? '')}</p>
        <p>{String(props.emptyState ?? '')}</p>
        {slot?.()}
      </div>
    )
  },
}))

vi.mock('./api', async () => {
  const actual = await vi.importActual<typeof import('./api')>('./api')
  return {
    ...actual,
    archiveAgentProfile: vi.fn(),
    axisCrudHttpClient: vi.fn(() => ({ json: vi.fn() })),
    listAgentProfiles: vi.fn(async () => [{
      profile_id: 'support.v1',
      family_id: 'support',
      version_label: 'v1',
      name: 'Support',
      max_autonomy: 'A2',
      enabled: true,
    }]),
    listIAMTenants: vi.fn(async () => [{ id: 'org-a', name: 'Org A', status: 'active' }]),
    listJobRoles: vi.fn(async () => [{
      job_role_id: 'billing-specialist',
      org_id: 'org-a',
      product_surface: 'axis',
      name: 'Billing Specialist',
      slug: 'billing-specialist',
      mission: 'Keep billing clean',
      responsibilities: [{ title: 'Review invoices' }],
      recommended_capabilities: ['billing.read'],
      default_autonomy_level: 'A2',
      status: 'active',
    }]),
    purgeAgentProfile: vi.fn(),
    archiveJobRole: vi.fn(),
    restoreJobRole: vi.fn(),
    restoreAgentProfile: vi.fn(),
    trashAgentProfile: vi.fn(),
    upsertAgentProfile: vi.fn(),
    upsertJobRole: vi.fn(),
  }
})

describe('AgentsControlCenter as Virtual Employees surface', () => {
  it('uses the Virtual Employees endpoint and public labels', async () => {
    crudPageProps.length = 0

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" />)

    expect(await screen.findByRole('button', { name: 'Virtual Employees' })).toBeInTheDocument()
    await waitFor(() => {
      expect(crudPageProps.at(-1)?.basePath).toBe(VIRTUAL_EMPLOYEES_BASE_PATH)
    })
    expect(screen.getAllByText('Virtual Employees').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText(VIRTUAL_EMPLOYEES_BASE_PATH)).toBeInTheDocument()
    expect(screen.getByText('Buscar virtual employees')).toBeInTheDocument()
    expect(screen.getByText('Sin virtual employees')).toBeInTheDocument()
  })

  it('maps semantic fields into metadata without dropping existing metadata', async () => {
    crudPageProps.length = 0

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" />)

    await waitFor(() => {
      expect(crudPageProps.at(-1)?.basePath).toBe(VIRTUAL_EMPLOYEES_BASE_PATH)
    })
    const props = crudPageProps.at(-1) as {
      formFields: Array<{ key: string; label: string }>
      toFormValues: (row: Record<string, unknown>) => Record<string, string | boolean>
      toBody: (values: Record<string, string | boolean>) => Record<string, unknown>
    }
    expect(props.formFields.map((field) => field.label)).toEqual(expect.arrayContaining([
      'Puesto / Job title',
      'Misión',
      'Responsabilidades',
      'Owner humano',
      'Canales de contacto',
      'Reglas de escalamiento',
    ]))

    const formValues = props.toFormValues({
      id: 'employee-1',
      org_id: 'org-a',
      name: 'Finance Employee',
      profile: 'finance.v1',
      autonomy: 'A2',
      memory_enabled: true,
      description: 'Normal description',
      capabilities: ['billing.read'],
      tools: ['billing_read'],
      metadata: {
        custom_flag: 'keep-me',
        job_title: 'Finance Coordinator',
        mission: 'Close monthly billing',
        responsibilities: ['review invoices', 'escalate blockers'],
        owner_user_id: 'user-123',
        contact_channels: ['slack:#finance'],
        escalation_rules: ['manager after 2 days'],
      },
    })

    expect(formValues.job_title).toBe('Finance Coordinator')
    expect(formValues.mission).toBe('Close monthly billing')
    expect(formValues.responsibilities).toBe('review invoices\nescalate blockers')
    const body = props.toBody({
      ...formValues,
      name: 'Finance Lead',
      profile: 'finance.v2',
      autonomy: 'A3',
    })
    expect(body.name).toBe('Finance Lead')
    expect(body.metadata).toEqual({
      custom_flag: 'keep-me',
      job_title: 'Finance Coordinator',
      mission: 'Close monthly billing',
      responsibilities: ['review invoices', 'escalate blockers'],
      owner_user_id: 'user-123',
      contact_channels: ['slack:#finance'],
      escalation_rules: ['manager after 2 days'],
    })
  })

  it('does not apply Job Role defaults when editing an existing Virtual Employee', async () => {
    crudPageProps.length = 0

    render(<AgentsControlCenter orgId="org-a" tenantId="tenant-a" />)

    await waitFor(() => {
      const props = crudPageProps.at(-1) as {
        basePath?: string
        toBody?: (values: Record<string, string | boolean>) => Record<string, unknown>
      }
      expect(props.basePath).toBe(VIRTUAL_EMPLOYEES_BASE_PATH)
      expect(props.toBody?.({
        name: 'Billing Employee',
        profile: 'support.v1',
        autonomy: 'A2',
        memory_enabled: true,
        description: '',
        job_role_id: 'billing-specialist',
        job_title: '',
        mission: '',
        responsibilities: '',
        capabilities: '',
        tools: '',
      }).metadata).toMatchObject({
        job_role_id: 'billing-specialist',
        job_title: 'Billing Specialist',
        mission: 'Keep billing clean',
        responsibilities: ['Review invoices'],
      })
    })

    const props = crudPageProps.at(-1) as {
      toBody: (values: Record<string, string | boolean>) => Record<string, unknown>
    }
    const body = props.toBody({
      _metadata_json: '{}',
      name: 'Billing Employee',
      profile: 'support.v1',
      autonomy: 'A2',
      memory_enabled: true,
      description: '',
      job_role_id: 'billing-specialist',
      job_title: '',
      mission: '',
      responsibilities: '',
      capabilities: '',
      tools: '',
    })

    expect(body.metadata).toEqual({ job_role_id: 'billing-specialist' })
    expect(body.capabilities).toEqual([])
  })
})
