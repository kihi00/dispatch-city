# Demo: Pod-Admin-Controls (Block 7 Bonus)

Diese Seite belegt, dass die Admin-Buttons im Dashboard (⟲ Restart, + / − Skalieren, 💀 Chaos-Kill) echte, verifizierbare Aktionen im Kubernetes-Cluster auslösen — nicht nur kosmetisch im Frontend simuliert sind. Jeder Abschnitt zeigt GUI-Screenshot und `kubectl`-Beleg nebeneinander.

## Übersicht

Die Systemansicht zeigt live den Zustand aller Deployments. Bei `control-api` und `order-worker` (sowie den drei Restaurant-Deployments) stehen zusätzlich Steuer-Buttons zur Verfügung, dazu die tatsächlichen Pod-Namen als Live-Badges (grün = ready).

<img width="524" height="334" alt="image" src="https://github.com/user-attachments/assets/d60de183-e0eb-4fa2-a4d8-22dda6540a2c" />


## 1. Restart

Klick auf ⟲ löst einen Rolling Restart aus (technisch: eine `restartedAt`-Annotation wird ins Pod-Template gepatcht, wodurch Kubernetes ein neues ReplicaSet erstellt und die Pods nacheinander austauscht).

<img width="88" height="63" alt="image" src="https://github.com/user-attachments/assets/1a671d92-cb21-4136-9bb6-0d92b55d17e6" />


**Beobachtung im Terminal** (`kubectl -n food-delivery get pods -l app.kubernetes.io/name=control-api -w`), direkt nach einem Restart-Klick:

```
NAME                           READY   STATUS             RESTARTS   AGE
control-api-5597709890-2qngf  1/1     Running            0          2m8s
control-api-5c5f78dd5-hfw92   0/1     Pending            0          0s
control-api-5c5f78dd5-hfw92   0/1     ContainerCreating  0          2s
control-api-5597709890-2qngf  1/1     Terminating        0          2m10s
control-api-5c5f78dd5-hfw92   1/1     Running            0          14s
control-api-5597709890-2qngf  0/1     Completed          0          2m10s
```

Zu sehen: ein neuer Pod (`5c5f78dd5-hfw92`) durchläuft `Pending` → `ContainerCreating` → `Running`, während der alte Pod (`5597709890-2qngf`) parallel `Terminating` → `Completed` durchläuft. Rollierender Austausch, kein gleichzeitiger Ausfall aller Pods.

<img width="96" height="95" alt="image" src="https://github.com/user-attachments/assets/639f165b-14e7-490d-b0a3-21089ae18a1f" />

## 2. Skalieren (Scale Up / Down)

Klick auf `+`/`−` patcht `spec.replicas` direkt im Deployment.

**Vorher** (`kubectl -n food-delivery get deployment order-worker`):
```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
order-worker   3/3     3            3           27d
```

**Nachher**, nach einem Klick auf `−`:
```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
order-worker   1/1     1            1           27d
```

`READY` sinkt von `3/3` auf `1/1` — exakt der erwartete Effekt eines Scale-Down-Klicks, bestätigt direkt aus der Kubernetes-API, unabhängig vom Dashboard.

## 3. Chaos-Kill (Pod gezielt löschen)

Klick auf 💀 löscht einen zufälligen, laufenden Pod des gewählten Deployments. Kubernetes erkennt die Abweichung vom Soll-Zustand (`spec.replicas`) und erstellt automatisch einen Ersatz-Pod — das Kernprinzip von Kubernetes' Selbstheilung.

<img width="423" height="300" alt="image" src="https://github.com/user-attachments/assets/1ac3db24-11cd-43b6-aa86-555758fce766" />

Direkt nach dem Klick zeigt die grüne Toast-Meldung im Dashboard den Namen des gelöschten Pods. Zum Vergleich die Live-Pod-Namen-Badges unter der Karte, die sich unmittelbar danach aktualisieren (neuer Name erscheint, alter verschwindet).

## Fazit

Alle drei Aktionen wurden sowohl über die GUI als auch unabhängig per `kubectl` verifiziert. Die Zahlen und Pod-Namen im Dashboard stimmen mit dem tatsächlichen Cluster-Zustand überein — die Admin-Endpoints (`internal/cluster/controller.go`, `internal/api/server.go`) führen echte, autorisierte Schreiboperationen gegen die Kubernetes-API aus (RBAC: `deploy/overlays/block-07-observability/rbac-control-api-admin.yaml`).
