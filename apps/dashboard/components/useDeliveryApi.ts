import type { EventEnvelope, Snapshot } from '~/types/delivery'

export interface HpaStatus {
  name: string
  current_replicas: number
  desired_replicas: number
  min_replicas: number
  max_replicas: number
  cpu_percent?: number
}

const emptySnapshot = (): Snapshot => ({
  mode: 'standalone',
  running: false,
  tick: 0,
  instance: 'connecting',
  restaurants: [],
  customers: [],
  couriers: [],
  orders: [],
  components: [],
  stats: { active_orders: 0, delivered: 0, events: 0, ready_pods: 0, total_pods: 0 },
})

export function useDeliveryApi() {
  const config = useRuntimeConfig()
  const apiBase = config.public.apiBase as string
  const snapshot = useState<Snapshot>('delivery-snapshot', emptySnapshot)
  const events = useState<EventEnvelope[]>('delivery-events', () => [])
  const connection = useState<'connecting' | 'live' | 'offline'>('delivery-connection', () => 'connecting')
  const lastError = useState<string>('delivery-error', () => '')
  const hpaStatus = useState<HpaStatus[]>('delivery-hpa', () => [])
  const adminError = useState<string>('delivery-admin-error', () => '')
  let eventSource: EventSource | undefined
  let refreshTimer: ReturnType<typeof setTimeout> | undefined
  let pollTimer: ReturnType<typeof setInterval> | undefined
  let hpaTimer: ReturnType<typeof setInterval> | undefined

  const endpoint = (path: string) => `${apiBase}${path}`

  async function refreshSnapshot() {
    try {
      snapshot.value = await $fetch<Snapshot>(endpoint('/api/v1/snapshot'))
      lastError.value = ''
    } catch (error) {
      connection.value = 'offline'
      lastError.value = error instanceof Error ? error.message : 'Snapshot nicht erreichbar'
    }
  }

  async function refreshHpa() {
    try {
      hpaStatus.value = await $fetch<HpaStatus[]>(endpoint('/api/v1/hpa'))
    } catch {
      // Admin-Cluster evtl. nicht verfügbar (z.B. lokal ohne Kubernetes) — still, kein harter Fehler
      hpaStatus.value = []
    }
  }

  function scheduleRefresh() {
    if (refreshTimer) return
    refreshTimer = setTimeout(async () => {
      refreshTimer = undefined
      await refreshSnapshot()
    }, 180)
  }

  function connect() {
    if (!import.meta.client) return
    connection.value = 'connecting'
    eventSource = new EventSource(endpoint('/api/v1/events'))
    eventSource.onopen = () => {
      connection.value = 'live'
    }
    eventSource.onmessage = (message) => {
      try {
        const event = JSON.parse(message.data) as EventEnvelope
        const previous = event.event_type === 'courier.location.updated'
          ? events.value.filter(item => item.event_type !== 'courier.location.updated' || item.source !== event.source)
          : events.value
        events.value = [event, ...previous].slice(0, 40)
        scheduleRefresh()
      } catch {
        lastError.value = 'Ein Event konnte nicht gelesen werden.'
      }
    }
    eventSource.onerror = () => {
      connection.value = 'offline'
    }
  }

  async function command(path: string) {
    await $fetch(endpoint(path), { method: 'POST' })
    await refreshSnapshot()
  }

  // --- Admin-Aktionen (Block 7 Bonus) -----------------------------------

  async function restartPod(deployment: string) {
    adminError.value = ''
    try {
      await $fetch(endpoint(`/api/v1/pods/${deployment}/restart`), { method: 'POST' })
      await refreshSnapshot()
    } catch (error) {
      adminError.value = error instanceof Error ? error.message : 'Restart fehlgeschlagen'
    }
  }

  async function scalePod(deployment: string, replicas: number) {
    adminError.value = ''
    try {
      await $fetch(endpoint(`/api/v1/pods/${deployment}/scale`), {
        method: 'POST',
        body: { replicas },
      })
      await refreshSnapshot()
    } catch (error) {
      adminError.value = error instanceof Error ? error.message : 'Skalierung fehlgeschlagen'
    }
  }

  async function chaosPod(deployment: string) {
    adminError.value = ''
    try {
      await $fetch(endpoint(`/api/v1/pods/${deployment}/chaos`), { method: 'POST' })
      await refreshSnapshot()
    } catch (error) {
      adminError.value = error instanceof Error ? error.message : 'Chaos-Aktion fehlgeschlagen'
    }
  }

  onMounted(async () => {
    await refreshSnapshot()
    await refreshHpa()
    connect()
    pollTimer = setInterval(refreshSnapshot, 5000)
    hpaTimer = setInterval(refreshHpa, 5000)
  })

  onBeforeUnmount(() => {
    eventSource?.close()
    if (refreshTimer) clearTimeout(refreshTimer)
    if (pollTimer) clearInterval(pollTimer)
    if (hpaTimer) clearInterval(hpaTimer)
  })

  return {
    snapshot,
    events,
    connection,
    lastError,
    hpaStatus,
    adminError,
    refreshSnapshot,
    start: () => command('/api/v1/simulation/start'),
    pause: () => command('/api/v1/simulation/pause'),
    reset: () => command('/api/v1/simulation/reset'),
    createOrder: () => command('/api/v1/orders'),
    restartPod,
    scalePod,
    chaosPod,
  }
}
