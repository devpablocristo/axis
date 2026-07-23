import {
  BadgeCheck,
  BookKey,
  Boxes,
  FileCheck2,
  Gavel,
  History,
  ShieldAlert,
  Siren,
} from 'lucide-react'
import type { ReactNode } from 'react'

export type NexusDestination = 'approvals' | 'governance' | 'nexus-incidents' | 'nexus-retention' | 'operations'

type Props = {
  onNavigate: (destination: NexusDestination) => void
}

type NexusDomain = {
  title: string
  description: string
  capabilities: string[]
  icon: ReactNode
  destination?: NexusDestination
  action?: string
}

const domains: NexusDomain[] = [
  {
    title: 'Authorization engine',
    description: 'Decides whether an action can run and revalidates authority immediately before execution.',
    capabilities: ['Risk-based decisions', 'Authority snapshots', 'Execution result binding'],
    icon: <BadgeCheck aria-hidden="true" />,
    destination: 'governance',
    action: 'Open policies',
  },
  {
    title: 'Action types & risk',
    description: 'Registers governable action types, their baseline risk and whether they are enabled.',
    capabilities: ['Stable action keys', 'Risk floor', 'Global disable'],
    icon: <Boxes aria-hidden="true" />,
  },
  {
    title: 'Policies & access',
    description: 'Combines scoped functional grants with immutable, versioned CEL policies.',
    capabilities: ['RBAC grants', 'Shadow simulation', 'Independent promotion'],
    icon: <BookKey aria-hidden="true" />,
    destination: 'governance',
    action: 'Manage policies & access',
  },
  {
    title: 'Approvals',
    description: 'Manages human decisions for actions that cannot execute autonomously.',
    capabilities: ['Approve or reject', 'No self-approval', 'Dual-control break glass'],
    icon: <Gavel aria-hidden="true" />,
    destination: 'approvals',
    action: 'Review approvals',
  },
  {
    title: 'Audit & evidence',
    description: 'Maintains verifiable audit chains and produces signed, tamper-evident evidence packs.',
    capabilities: ['Append-only events', 'Replay and verification', 'Attestation checks'],
    icon: <History aria-hidden="true" />,
  },
  {
    title: 'Incidents & SLOs',
    description: 'Turns repeated governance and runtime findings into durable operational incidents.',
    capabilities: ['Incident lifecycle', 'SLO evaluation', 'Metadata-only notifications'],
    icon: <Siren aria-hidden="true" />,
    destination: 'nexus-incidents',
    action: 'Open incidents & SLOs',
  },
  {
    title: 'Preservation & exports',
    description: 'Protects governed records from deletion and creates integrity-bound enterprise exports.',
    capabilities: ['Legal holds', 'Complete-or-fail exports', 'Manifest hashes'],
    icon: <FileCheck2 aria-hidden="true" />,
    destination: 'nexus-retention',
    action: 'Open holds & exports',
  },
  {
    title: 'Nexus operations',
    description: 'Runs durable jobs, reconciliation, outbox delivery and shared worker controls.',
    capabilities: ['Idempotent jobs', 'Safe reconciliation', 'Circuit breakers'],
    icon: <ShieldAlert aria-hidden="true" />,
    destination: 'operations',
    action: 'Open runtime operations',
  },
]

export function NexusOverviewPage({ onNavigate }: Props) {
  return (
    <section className="nexus-overview">
      <div className="nexus-overview__grid">
        {domains.map((domain) => (
          <article className="card nexus-domain-card" key={domain.title}>
            <div className="nexus-domain-card__heading">
              <span>{domain.icon}</span>
              <div>
                <h3>{domain.title}</h3>
                <p>{domain.description}</p>
              </div>
            </div>
            <ul>
              {domain.capabilities.map((capability) => <li key={capability}>{capability}</li>)}
            </ul>
            {domain.destination && domain.action ? (
              <button className="btn-secondary" type="button" onClick={() => onNavigate(domain.destination!)}>
                {domain.action}
              </button>
            ) : <small>Runtime-owned surface</small>}
          </article>
        ))}
      </div>
    </section>
  )
}
