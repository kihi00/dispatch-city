# Dispatch City

Visuelles Food-Delivery-System als verteilte Kubernetes-Anwendung. Kunden erzeugen Bestellungen als Events, Restaurant-Worker nehmen sie an, Kuriere liefern sichtbar aus, der Order Worker hält den fachlichen Zustand konsistent in PostgreSQL. Das Dashboard zeigt fachliche Vorgänge und Clusterzustand in einer Live-Ansicht.

Transferarbeit VSC-01, Kilian Hirschi und Kim Flückiger, TEKO Olten.

| Baustein                         | Technologie                                       |
| -------------------------------- | ------------------------------------------------- |
| Dashboard                        | Nuxt, PixiJS, eigenes Image                       |
| Control API, Simulatoren, Worker | Go, eigene Images                                 |
| Messaging                        | RabbitMQ, Topic Exchange, DLQ                     |
| Datenhaltung                     | PostgreSQL via CloudNativePG, Primary und Standby |
| Observability                    | kube-prometheus-stack, Grafana, cluster-observer  |
| Manifeste                        | Kustomize, ein Overlay je Ausbaustufe             |

Architektur je Ausbaustufe: [docs/architecture.md](docs/architecture.md).

## Voraussetzungen

Docker, k3d, kubectl, helm, go und node.

```bash
docker version && k3d version && kubectl version --client && helm version && go version && node --version
```

Der Cluster heisst `teko-k8s`. Alle Skripte verwenden `teko-k8s` bzw. `k3d-teko-k8s` als Vorgabe. Ein anderer Name ist über `CLUSTER` und `CONTEXT` möglich, zum Beispiel `CLUSTER=eigen CONTEXT=k3d-eigen sh platform/monitoring/start-course.sh`.

## Schnellstart

### 1. Repository

```bash
git clone https://github.com/kihi00/dispatch-city.git
cd dispatch-city
```

Das Dashboard wird im Container gebaut, `apps/dashboard/node_modules` wird lokal nicht benötigt. Für lokale Entwicklung `cd apps/dashboard && npm ci`.

### 2. Cluster

```bash
k3d cluster create teko-k8s --agents 2
kubectl config use-context k3d-teko-k8s
```

Ein bestehender, gestoppter Cluster wird mit `k3d cluster start teko-k8s` wieder hochgefahren.

### 3. Images bauen und importieren

```bash
sh scripts/build-images.sh                                    # Go-Dienste
docker build -t food-delivery-dashboard:local apps/dashboard  # Dashboard
sh scripts/load-images.sh                                     # Import in den Cluster
```

Windows / PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
./scripts/build-images.ps1
docker build -t food-delivery-dashboard:local apps/dashboard
./scripts/load-images.ps1
```

### 4. Datenbank-Operator

```bash
sh platform/cloudnative-pg/install.sh     # Windows: ./platform/cloudnative-pg/install.ps1
```

Helm installiert den CloudNativePG-Operator im Namespace `cnpg-system`. Die Datenbank selbst entsteht erst aus der Cluster-Resource im nächsten Schritt.

### 5. Anwendung bis Ausbaustufe 6

```bash
kubectl apply -k deploy/overlays/block-06-persistence
kubectl -n food-delivery get cluster food-delivery-db -w      # bis "Cluster in healthy state", dann Ctrl+C
```

### 6. Monitoring und Ausbaustufe 7

```bash
sh platform/monitoring/start-course.sh    # Windows: ./platform/monitoring/start-course.ps1
```

Baut den cluster-observer, importiert ihn, installiert den kube-prometheus-stack und wendet das Overlay `block-07-observability` an. Der erste Durchlauf dauert einige Minuten.

### 7. Zugriff

Je ein eigenes Terminal, bleibt offen:

```bash
kubectl -n kube-system port-forward service/traefik 8081:80
kubectl -n monitoring port-forward service/monitoring-grafana 3000:80
kubectl -n food-delivery port-forward service/rabbitmq 15672:15672
```

| Adresse                | Inhalt                                       | Anmeldung           |
| ---------------------- | -------------------------------------------- | ------------------- |
| http://localhost:8081  | Dispatch City, API unter`/api`             | —                  |
| http://localhost:3000  | Grafana, Dashboard "Dispatch City - Betrieb" | admin / delivery    |
| http://localhost:15672 | RabbitMQ Management, Queues und Backlog      | delivery / delivery |

Port-Forwards überleben einen Cluster-Neustart nicht und müssen danach neu gestartet werden.

## Smoke-Test

```bash
kubectl -n food-delivery get pods                                   # alle Pods Running und ready
kubectl -n food-delivery get cluster food-delivery-db               # Cluster in healthy state
kubectl -n monitoring get pods                                      # Prometheus und Grafana Running

curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/                    # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/api/v1/snapshot     # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/health/ready        # 200

kubectl -n food-delivery exec rabbitmq-0 -- rabbitmqctl list_queues name messages_ready consumers
```

Erwartet: `messages_ready` bleibt bei den fachlichen Queues nahe null, jede Restaurant-Queue hat einen Consumer, `order-projection` hat zwei.

## Reset

```bash
kubectl delete -k deploy/overlays/block-07-observability      # Anwendung entfernen, Cluster bleibt
k3d cluster stop teko-k8s                                     # Cluster anhalten, Daten bleiben
k3d cluster delete teko-k8s                                   # Cluster samt Daten entfernen
kubectl delete namespace betrieb-lab                          # Übungs-Namespace aus Block 7
```

Nach `k3d cluster delete` beginnt der Aufbau wieder bei Schritt 2.

## Projektstruktur

```
apps/dashboard/        Nuxt-Dashboard mit PixiJS
cmd/                   Go-Dienste: control-api, customer-simulator, restaurant-worker,
                       courier-simulator, order-worker, migrate, cluster-observer
internal/              Messaging, Persistenz, API, Cluster-Beobachtung, Telemetrie
build/                 Dockerfile für die Go-Dienste
deploy/base/           Namespace, ConfigMap, Deployments, Services
deploy/overlays/       ein Overlay je Ausbaustufe, block-03-standalone bis block-07-observability
platform/              Helm-Installationen: CloudNativePG und kube-prometheus-stack
scripts/               Build, Import und Labor-Skripte
labs/block-07/         Übungen zu Ressourcen und HPA im Namespace betrieb-lab
docs/                  Architektur und Reflexion
```

## Herkunft des Codes und Hilfsmittel

- Grundlage ist der Kurs-Starter von Patrick Michel sowie die Kurs-Pakete je Block (`vsc-dispatch-city-04-ingress`, `-05-messaging`, `-06-persistence`, `-07-observability`), die über die beiliegenden Installationsskripte integriert wurden. Die Go-Dienste, das Dashboard und die Overlays ab Block 4 stammen aus diesen Paketen.
- Eigene Arbeit: Integration und Betrieb der Ausbaustufen, Anpassungen am Monitoring-Values-File, HPA-Übung, diese Dokumentation, Architektur- und Reflexionsnotizen sowie die Commit-Historie je Block.
- KI-Unterstützung: Claude für Dokumentation, Fehlersuche und Erklärungen zu Kubernetes-Konzepten. Auch die zusätzliche Erweiterung über die Pflichtanforderungen hinaus ist mit KI-Unterstützung entstanden: Konzept, Manifeste und Beschreibung haben wir gemeinsam mit Claude erarbeitet. Der eingesetzte Code wurde jeweils selbst ausgeführt, überprüft und im Cluster nachvollzogen.

## Aufgabenverteilung

Wir haben alle Ausbaustufen gemeinsam erarbeitet: Aufgaben lesen, Manifeste durchgehen, deployen, Fehler suchen und die Ergebnisse im Cluster nachvollziehen. Gearbeitet wurde abwechselnd auf der einen und auf der anderen Maschine, jeweils zu zweit am selben Stand. Entsprechend committet die Person, auf deren Maschine der Block entstanden ist.

Die Zuordnung ist in der Historie sichtbar: `git log --format='%h %an %s'`.

## Weiterführend

- [docs/architecture.md](docs/architecture.md) — Komponenten, Eventfluss, Datenhaltung, Entscheidungen
- [docs/reflexion.md](docs/reflexion.md) — Grenzen, beobachtete Fehlerbilder, Verbesserungen
- [docs/demo/](docs/demo/) — Screenshots der finalen Demo
