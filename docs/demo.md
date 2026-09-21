# Demo: Pod-Admin-Controls (Block 7 Bonus)

Diese Seite belegt, dass die Admin-Buttons im Dashboard (⟲ Restart, + / − Skalieren, 💀 Chaos-Kill) echte, verifizierbare Aktionen im Kubernetes-Cluster auslösen — nicht nur kosmetisch im Frontend simuliert sind. Zwei getrennte Use-Cases: einmal per GUI-Screenshot belegt (Order Worker), einmal per durchgehender Terminal-Live-Beobachtung (Control API).

## Übersicht

Die Systemansicht zeigt live den Zustand aller Deployments. Bei `control-api` und `order-worker` (sowie den drei Restaurant-Deployments) stehen zusätzlich Steuer-Buttons zur Verfügung, dazu die tatsächlichen Pod-Namen als Live-Badges (grün = ready).

<img width="524" height="334" alt="Systemübersicht mit Admin-Controls" src="https://github.com/user-attachments/assets/d60de183-e0eb-4fa2-a4d8-22dda6540a2c" />

---

## Use Case 1: Order Worker (GUI-Screenshots)

Restart-Klick bei `order-worker` im Dashboard:

<img width="88" height="63" alt="image" src="https://github.com/user-attachments/assets/a106dc38-118f-4701-972c-2d340bf19d45" />

<img width="96" height="95" alt="image" src="https://github.com/user-attachments/assets/0a17ebf1-f459-4448-9ab4-030ca964350b" />

**Skalierung, unabhängig per `kubectl` verifiziert** — `kubectl -n food-delivery get deployment order-worker`:

Vorher:
```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
order-worker   3/3     3            3           27d
```

Nachher, nach einem Klick auf `−`:
```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
order-worker   1/1     1            1           27d
```

`READY` sinkt von `3/3` auf `1/1` — exakt der erwartete Effekt eines Scale-Down-Klicks.

---

## Use Case 2: Control API (durchgehende Terminal-Live-Beobachtung)

Statt einzelner Vorher/Nachher-Screenshots wurde hier eine durchgehende `kubectl get pods -w`-Sitzung mitgeschnitten, während im Dashboard nacheinander **Scale runter, Scale hoch, Restart und Chaos-Kill** bei `control-api` geklickt wurden. Das Log zeigt alle vier Aktionen im echten Cluster-Verhalten:

<img width="423" height="300" alt="image" src="https://github.com/user-attachments/assets/0fce68cf-dfeb-4020-af39-c92c49e472ac" />

**Wie man die einzelnen Aktionen im Log unterscheidet:**

- **Restart** → ein neuer `pod-template-hash` taucht auf (z. B. `5597709890` → `5c5f78dd5`). Alle Pods des alten Hash gehen `Terminating` → `Completed`, während neue Pods mit dem neuen Hash `Pending` → `ContainerCreating` → `Running` durchlaufen. Rollierend, nie alle gleichzeitig down.
- **Scale up** → mehrere neue Pods (gleicher Hash wie die bereits laufenden) erscheinen gleichzeitig bei `Pending`, ohne dass alte Pods verschwinden.
- **Scale down** → einzelne Pods gehen `Terminating` → `Completed`, ohne dass neue nachkommen, und die Gesamtzahl bleibt danach dauerhaft niedriger.
- **Chaos-Kill** → **ein einzelner** Pod (gleicher Hash wie die übrigen, die weiterlaufen) geht unerwartet `Terminating`, während alle anderen ungestört `Running` bleiben — direkt danach erscheint automatisch ein Ersatz-Pod mit demselben Hash. Der Unterschied zu Restart: nur einer betroffen, nicht das ganze ReplicaSet.

## Fazit

Alle Aktionen wurden sowohl über die GUI als auch unabhängig per `kubectl` verifiziert. Die Zahlen und Pod-Namen im Dashboard stimmen mit dem tatsächlichen Cluster-Zustand überein — die Admin-Endpoints (`internal/cluster/controller.go`, `internal/api/server.go`) führen echte, autorisierte Schreiboperationen gegen die Kubernetes-API aus (RBAC: `deploy/overlays/block-07-observability/rbac-control-api-admin.yaml`).
