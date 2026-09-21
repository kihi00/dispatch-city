<script setup lang="ts">
import { Activity, Boxes, Database, Globe2, MessageSquareMore, RefreshCw, Server, SkullIcon, ZapOff } from '@lucide/vue'
import type { ComponentStatus } from '~/types/delivery'
import { useDeliveryApi } from '~/composables/useDeliveryApi'

defineProps<{ components: ComponentStatus[] }>()

const { hpaStatus, adminError, adminNotice, podInstances, restartPod, scalePod, chaosPod } = useDeliveryApi()

// Live-Zahl direkt aus Kubernetes (podInstances) hat Vorrang vor der
// verzögerten Snapshot-Zahl (die über cluster-observer relayed wird).
function liveReady(component: ComponentStatus): number {
  return podInstances.value[component.id]?.ready ?? component.ready
}
function liveDesired(component: ComponentStatus): number {
  return podInstances.value[component.id]?.desired ?? component.desired
}

const iconFor = (id: string) => {
  if (id.includes('dashboard')) return Globe2
  if (id.includes('rabbit')) return MessageSquareMore
  if (id.includes('postgre')) return Database
  if (id.includes('worker')) return Boxes
  if (id.includes('api')) return Server
  return Activity
}

// Nur diese Deployments sind über die Admin-Endpoints steuerbar
// (siehe allowedAdminDeployments in internal/api/server.go).
const controllable = new Set(['control-api', 'order-worker', 'restaurant-pizza', 'restaurant-bowl', 'restaurant-curry'])

const busy = ref<string | null>(null)

async function onRestart(id: string) {
  busy.value = id
  await restartPod(id)
  busy.value = null
}

async function onScale(id: string, current: number, delta: number) {
  const next = Math.max(0, Math.min(5, current + delta))
  busy.value = id
  await scalePod(id, next)
  busy.value = null
}

async function onChaos(id: string) {
  if (!confirm(`Wirklich einen Pod von "${id}" löschen? (Simuliert Ausfall)`)) return
  busy.value = id
  await chaosPod(id)
  busy.value = null
}
</script>

<template>
  <section class="system-map" aria-label="Technische Systemansicht">
    <div class="system-map__backdrop">
      <span v-for="index in 14" :key="index" />
    </div>
    <div class="system-map__flow system-map__flow--one">HTTP / SSE</div>
    <div class="system-map__flow system-map__flow--two">AMQP events</div>
    <div class="system-map__flow system-map__flow--three">SQL projection</div>
    <article
      v-for="(component, index) in components"
      :key="component.id"
      class="system-node"
      :class="[`system-node--${component.category}`, { 'system-node--planned': component.status === 'planned' }]"
      :style="{ '--node-index': index }"
    >
      <component :is="iconFor(component.id)" :size="22" stroke-width="1.8" />
      <div>
        <span>{{ component.kind }}</span>
        <strong>{{ component.name }}</strong>
        <small>{{ component.detail }}</small>
      </div>
      <div class="replica-count" :class="{ 'replica-count--ok': liveReady(component) === liveDesired(component) && liveDesired(component) > 0 }">
        {{ liveReady(component) }}/{{ liveDesired(component) }}
      </div>

      <div v-if="controllable.has(component.id) && podInstances[component.id]?.pods?.length" class="pod-names">
        <span
          v-for="pod in podInstances[component.id].pods"
          :key="pod.name"
          class="pod-name"
          :class="{ 'pod-name--ready': pod.ready }"
        >
          {{ pod.name }}
        </span>
      </div>

      <div v-if="controllable.has(component.id)" class="admin-controls">
        <button
          type="button"
          :disabled="busy === component.id"
          title="Neu starten"
          aria-label="Neu starten"
          @click="onRestart(component.id)"
        >
          <RefreshCw :size="14" />
        </button>
        <button
          type="button"
          :disabled="busy === component.id"
          title="Hochskalieren"
          aria-label="Hochskalieren"
          @click="onScale(component.id, component.desired, 1)"
        >
          +
        </button>
        <button
          type="button"
          :disabled="busy === component.id || component.desired <= 0"
          title="Runterskalieren"
          aria-label="Runterskalieren"
          @click="onScale(component.id, component.desired, -1)"
        >
          −
        </button>
        <button
          type="button"
          class="admin-controls__chaos"
          :disabled="busy === component.id || component.ready === 0"
          title="Chaos: zufälligen Pod löschen"
          aria-label="Chaos: zufälligen Pod löschen"
          @click="onChaos(component.id)"
        >
          <SkullIcon :size="14" />
        </button>
      </div>
    </article>

    <article v-if="hpaStatus.length" class="hpa-panel">
      <div class="hpa-panel__header">
        <ZapOff :size="18" stroke-width="1.8" />
        <strong>Autoscaler</strong>
      </div>
      <div v-for="hpa in hpaStatus" :key="hpa.name" class="hpa-row">
        <span>{{ hpa.name }}</span>
        <span>
          {{ hpa.current_replicas }}/{{ hpa.max_replicas }}
          <template v-if="hpa.cpu_percent !== undefined"> · CPU {{ hpa.cpu_percent }}%</template>
        </span>
        <div class="hpa-bar">
          <div
            class="hpa-bar__fill"
            :style="{ width: `${Math.min(100, (hpa.current_replicas / Math.max(1, hpa.max_replicas)) * 100)}%` }"
          />
        </div>
      </div>
    </article>

    <p v-if="adminNotice" class="admin-notice" role="status">{{ adminNotice }}</p>
    <p v-if="adminError" class="admin-error" role="status">{{ adminError }}</p>
  </section>
</template>

<style scoped>
.pod-names {
  /* .system-node ist ein Dreispalten-Grid - ohne dies landet die Liste in einer Spalte */
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 8px;
}
.pod-name {
  font-family: monospace;
  font-size: 10px;
  /* .system-node span vererbt uppercase - Podnamen sind aber Kleinschreibung */
  text-transform: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.5);
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.pod-name--ready {
  color: #3ecf8e;
  border-color: rgba(62, 207, 142, 0.3);
}
.admin-controls {
  grid-column: 1 / -1;
  display: flex;
  gap: 4px;
  margin-top: 8px;
}
.admin-controls button {
  flex: 1;
  border: 1px solid rgba(255, 255, 255, 0.15);
  background: transparent;
  border-radius: 6px;
  padding: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.admin-controls button:disabled {
  opacity: 0.4;
  cursor: default;
}
.admin-controls__chaos {
  color: #e5484d;
  border-color: rgba(229, 72, 77, 0.4) !important;
}
.hpa-panel {
  grid-column: 1 / -1;
  margin-top: 12px;
  padding: 12px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.12);
}
.hpa-panel__header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 14px;
}
.hpa-row {
  display: grid;
  grid-template-columns: 1fr auto;
  font-size: 13px;
  margin-bottom: 4px;
  gap: 4px;
}
.hpa-bar {
  grid-column: 1 / -1;
  height: 5px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.1);
  overflow: hidden;
}
.hpa-bar__fill {
  height: 100%;
  background: #378adf;
}
.admin-notice {
  grid-column: 1 / -1;
  font-size: 13px;
  color: #3ecf8e;
}
.admin-error {
  grid-column: 1 / -1;
  font-size: 13px;
  color: #e5484d;
}
</style>
