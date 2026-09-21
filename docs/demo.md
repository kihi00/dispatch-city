# Demo-Nachweis

Alle Bilder sind Bildschirmaufnahmen vom 21.09.2026. Die Bilder zu den Steuerknöpfen stammen von einem zweiten Cluster unter Windows, mit derselben Anwendung aus dem Repository.

## Die vier Oberflächen

### Dashboard

Die Stadtansicht zeigt den fachlichen Ablauf: Bestellungen, Kuriere unterwegs, gelieferte Aufträge und den Event-Stream rechts.

![Dashboard, Stadtansicht](demo/01_dashboard_stadt.png)

Die Systemansicht zeigt dieselbe Anwendung technisch. Bei Control API und Order Worker stehen die laufenden Pods mit Namen (grün = bereit) und die Steuerknöpfe unserer Erweiterung.

![Dashboard, Systemansicht](demo/02_dashboard_system.png)

### RabbitMQ

Eine Queue je Zweck: je Restaurant, für die Kurierzuteilung, für die Projektion in die Datenbank und je Control-API-Pod eine eigene Live-Queue. In `food.dead` liegen Nachrichten aus den DLQ-Übungen und aus den drei Failover-Tests weiter unten.

![RabbitMQ, Queues](demo/03_rabbitmq_queues.png)

### Grafana

Das Dashboard «Dispatch City - Betrieb» mit offenen und gelieferten Bestellungen, bereiten Pizza-Workern, wartenden Nachrichten je Restaurant und dem Durchsatz.

![Grafana, Betrieb](demo/04_grafana_betrieb.png)

### PostgreSQL / CloudNativePG

Die Datenbank läuft als CloudNativePG-Cluster mit einem Primary und einer Replica. Der Zustand ist im Failover-Szenario weiter unten dokumentiert.

## Die Steuerknöpfe im Dashboard

Die vier Knöpfe auf den Karten rufen Admin-Endpoints der Control API auf. Die Control API schreibt dann direkt gegen die Kubernetes-API. Ein Klick ändert also den echten Cluster, nicht nur die Anzeige.

| Knopf | Wirkung im Cluster |
|---|---|
| Neustart (Pfeil) | setzt im Pod-Template die Annotation `kubectl.kubernetes.io/restartedAt`, wie `kubectl rollout restart` |
| Plus / Minus | setzt `spec.replicas` auf den Sollwert der Karte plus oder minus eins. Die Karte zeigt immer 2, Plus setzt also 3 und Minus 1, ein zweiter Klick ändert nichts mehr |
| Totenkopf | löscht nach einer Rückfrage einen zufällig gewählten Pod des Deployments |

Damit die Control API das darf, läuft sie unter einem eigenen ServiceAccount. Dessen Role erlaubt neben Leserechten nur, Deployments zu patchen und Pods aufzulisten und zu löschen (`deploy/overlays/bonus-pod-admin/rbac-control-api-admin.yaml`). Zusätzlich lässt der Code nur fünf Deployments zu: Control API, Order Worker und die drei Restaurants (`internal/api/server.go`). Die Aufrufe gegen Kubernetes stehen in `internal/cluster/controller.go`.

### Demo: Pod-Admin-Controls (Block 7 Bonus)

Diese Seite belegt, dass die Admin-Buttons im Dashboard (⟲ Restart, + / − Skalieren, 💀 Chaos-Kill) echte, verifizierbare Aktionen im Kubernetes-Cluster auslösen — nicht nur kosmetisch im Frontend simuliert sind. Zwei getrennte Use-Cases: einmal per GUI-Screenshot belegt (Order Worker), einmal per durchgehender Terminal-Live-Beobachtung (Control API).

#### Übersicht

Die Systemansicht zeigt live den Zustand aller Deployments. Bei `control-api` und `order-worker` stehen zusätzlich Steuer-Buttons zur Verfügung, dazu die tatsächlichen Pod-Namen als Live-Badges (grün = ready).

<img width="524" height="334" alt="Systemübersicht mit Admin-Controls" src="https://github.com/user-attachments/assets/d60de183-e0eb-4fa2-a4d8-22dda6540a2c" />

---

#### Use Case 1: Order Worker (GUI-Screenshots)

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

#### Use Case 2: Control API (durchgehende Terminal-Live-Beobachtung)

Statt einzelner Vorher/Nachher-Screenshots wurde hier eine durchgehende `kubectl get pods -w`-Sitzung mitgeschnitten, während im Dashboard nacheinander **Scale runter, Restart, Chaos-Kill und Scale hoch** bei `control-api` geklickt wurden. Das Log zeigt alle vier Aktionen im echten Cluster-Verhalten:

<img width="423" height="300" alt="image" src="https://github.com/user-attachments/assets/0fce68cf-dfeb-4020-af39-c92c49e472ac" />

**Wie man die einzelnen Aktionen im Log unterscheidet:**

- **Restart** → ein neuer `pod-template-hash` taucht auf (z. B. `5597709890` → `5c5f78dd5`). Alle Pods des alten Hash gehen `Terminating` → `Completed`, während neue Pods mit dem neuen Hash `Pending` → `ContainerCreating` → `Running` durchlaufen. Rollierend, nie alle gleichzeitig down.
- **Scale up** → mehrere neue Pods (gleicher Hash wie die bereits laufenden) erscheinen gleichzeitig bei `Pending`, ohne dass alte Pods verschwinden.
- **Scale down** → einzelne Pods gehen `Terminating` → `Completed`, ohne dass neue nachkommen, und die Gesamtzahl bleibt danach dauerhaft niedriger.
- **Chaos-Kill** → **ein einzelner** Pod (gleicher Hash wie die übrigen, die weiterlaufen) geht unerwartet `Terminating`, während alle anderen ungestört `Running` bleiben — direkt danach erscheint automatisch ein Ersatz-Pod mit demselben Hash. Der Unterschied zu Restart: nur einer betroffen, nicht das ganze ReplicaSet.

#### Fazit

Alle Aktionen wurden sowohl über die GUI als auch unabhängig per `kubectl` verifiziert. Die Zahlen und Pod-Namen im Dashboard stimmen mit dem tatsächlichen Cluster-Zustand überein — die Admin-Endpoints (`internal/cluster/controller.go`, `internal/api/server.go`) führen echte, autorisierte Schreiboperationen gegen die Kubernetes-API aus (RBAC: `deploy/overlays/bonus-pod-admin/rbac-control-api-admin.yaml`).

## Szenario 1: Pod fällt aus, Kubernetes ersetzt ihn

Der Totenkopf-Knopf löscht einen Pod des gewählten Deployments. Vorher fragt das Dashboard nach. «Simuliert Ausfall» heisst hier: Der Pod wird wirklich gelöscht, der Ausfall ist echt, nur absichtlich ausgelöst.

<img src="demo/05_selbstheilung_dialog.png" alt="Rückfrage vor dem Löschen" width="65%">

Nach dem Klick beim Order Worker zeigt die Karte `1/2`, der neue Pod ist noch grau, und unten steht, welcher Pod gelöscht wurde.

![Dashboard direkt nach dem Klick](demo/06_selbstheilung_klick.png)

Im Terminal ist der Ersatz nach drei Sekunden schon da, zunächst mit `0/1`, also noch nicht bereit.

![Ersatz-Pod erscheint](demo/07_selbstheilung_terminal.png)

Laut Pod-Status ist der neue Pod sieben Sekunden nach dem Start bereit. Eine Minute später steht das Deployment wieder bei `2/2`.

![Ersatz-Pod bereit](demo/08_selbstheilung_erholt.png)

![Systemansicht nach der Selbstheilung](demo/09_selbstheilung_dashboard.png)

## Szenario 2: Soll und Ist

Ein Klick auf Plus bei der Control API stellt sie auf drei Pods, nach wenigen Sekunden laufen alle drei (`3/3`). Das ändert nur den Cluster, nicht das Manifest. `kubectl diff` zeigt die Abweichung genau an.

![Abweichung vom Manifest](demo/13_drift_vorher.png)

`kubectl apply` setzt den Soll-Zustand aus dem Repository wieder durch, die Control API läuft wieder mit zwei Pods.

![Soll-Zustand wiederhergestellt](demo/14_drift_nachher.png)

## Szenario 3: Eine Küche fällt aus, die Bestellungen stauen sich

Die Pizza-Küche wird auf null skaliert, danach gehen über den Knopf «Bestellung» 30 Bestellungen ein. Sie verteilen sich reihum auf die drei Restaurants. Bei Pizza bleiben sie liegen, Bowl und Curry arbeiten ihren Teil sofort ab.

![Rückstau im Terminal](demo/15_rueckstau_terminal.png)

![Rückstau in RabbitMQ](demo/16_rueckstau_rabbitmq.png)

In Grafana steht «Pizza-Worker bereit» auf 0, die Pizza-Kurve steigt. Der Ausfall zeigt sich also nicht als Fehlermeldung, sondern als wachsende Warteschlange.

![Rückstau in Grafana](demo/17_rueckstau_grafana.png)

## Szenario 4: Hochskalieren löst den Stau auf

Die Pizza-Küche läuft jetzt dreifach.

![Drei Pizza-Worker](demo/18_skalierung_terminal.png)

RabbitMQ zeigt drei Consumer an derselben Queue. Sie teilen sich die Arbeit, jede Nachricht geht an genau einen von ihnen. Im Diagramm oben sieht man die Warteschlange erst stehen und dann auf null fallen.

![Drei Consumer an der Pizza-Queue](demo/19_skalierung_consumers.png)

Grafana zeigt den ganzen Verlauf: Anstieg, Abbau auf null, drei Worker bereit. Danach wurde die Küche wieder auf einen Worker zurückgesetzt.

![Rückstau aufgelöst](demo/20_rueckstau_aufgeloest_grafana.png)

## Szenario 5: Datenbank-Failover

Vorher ist `food-delivery-db-2` der Primary, `food-delivery-db-rw` zeigt auf dessen IP, und in der Datenbank liegen 5762 Bestellungen, alle seit der Einrichtung am 31.08.

![Vor dem Failover](demo/21_failover_vorher.png)

Um 18:04:33 wird der Primary gelöscht. Der Cluster steht danach gut drei Minuten auf «Failing over», dann übernimmt `food-delivery-db-1`, und um 18:07:53 ist der Cluster wieder gesund.

![Ablauf des Failovers](demo/22_failover_ablauf.png)

Nachher ist `food-delivery-db-1` Primary, `food-delivery-db-rw` zeigt auf die neue IP, und der alte Primary ist als Replica wieder dabei. Die Anwendung musste nichts umstellen, weil sie nur den Service-Namen kennt. Die Zahl der Bestellungen ist weiter gestiegen, auf 5777.

![Nach dem Failover](demo/23_failover_nachher.png)

## Was wir dabei beobachtet haben

**Der Failover dauert gut drei Minuten, nicht Sekunden.** Wir haben ihn an diesem Tag dreimal ausgelöst, jedes Mal mit demselben Ergebnis. Beim Löschen des Pods fährt PostgreSQL zuerst kontrolliert herunter und wartet, bis alle Clients sich abmelden. Die Connection-Pools von Control API und Order Worker tun das nicht von selbst, also wartet PostgreSQL das volle Zeitlimit `smartShutdownTimeout` von 180 Sekunden ab. Solange läuft die Replikation weiter, und der Operator darf die Replica nicht befördern, um nicht zwei Primaries gleichzeitig zu haben. Das Operator-Log sagt genau das: «Waiting for all WAL receivers to be down to elect a new primary». Bei einem echten Absturz des Knotens entfällt dieses Warten.

**Bei jedem Failover landen Events in der Dead Letter Queue.** Während der drei Minuten schreibt der Order Worker über seine offenen Verbindungen normal weiter. Erst wenn PostgreSQL diese nach 180 Sekunden trennt, schlagen für wenige Sekunden Schreibversuche fehl, bis `food-delivery-db-rw` auf den neuen Primary zeigt. Diese Nachrichten gehen bei uns ohne Wiederholung direkt in die DLQ. Bei den drei Durchläufen stieg `food.dead` um 7, 11 und 4 Nachrichten. Das ist die Schwäche, die wir in der Architekturdokumentation schon benannt haben, hier ist sie belegt.

**Der Engpass verschiebt sich.** Die Küchen arbeiten die Bestellungen schnell ab, danach warten sie beim einzigen Kurier. `courier-dispatch` hatte während der Aufnahmen rund 60 wartende Nachrichten, abgebaut wird eine pro Minute. Hochskalieren an einer Stelle beschleunigt nur bis zum nächsten Engpass.
