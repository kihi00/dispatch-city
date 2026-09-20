# Reflexion

Ein kurzer Rückblick auf das Projekt: was läuft, wo die Grenzen liegen, was uns im Betrieb um die Ohren geflogen ist und was wir beim nächsten Mal anders machen würden.

## Was gut funktioniert hat

Der schrittweise Aufbau über die Blöcke hat sich bewährt. Jede Stufe war für sich lauffähig, und weil jede Stufe ein eigenes Overlay hat, konnten wir jederzeit zurück auf einen älteren Stand. Besonders geholfen hat, dass das Dashboard von Anfang an sichtbar macht, was im Cluster passiert. Fehler merkt man dort schneller als in einer Logdatei.

Am meisten gelernt haben wir dort, wo etwas nicht sofort lief. Die Theorie aus dem Unterricht wurde erst dann greifbar, wenn wir selbst nach der Ursache suchen mussten.

## Wo das System an Grenzen stösst

Der ganze Cluster läuft auf einem Laptop. Wir sehen echtes Kubernetes-Verhalten wie Scheduling, Selbstheilung, Rollouts und Failover, aber von echter Hochverfügbarkeit ist das weit weg: Geht der Rechner aus, ist alles weg. Ein Multi-Host-Netzwerk oder einen Cloud-Load-Balancer haben wir nie getestet.

Ähnlich sieht es beim Broker aus. RabbitMQ läuft als ein einzelner Pod mit einem Volume. Wartende Nachrichten überleben zwar einen Neustart, aber wenn der Broker weg ist, steht der ganze Eventfluss. Persistenz ist eben nicht Hochverfügbarkeit, das war eine der Aussagen, die im Unterricht erst trocken klang und beim Testen dann einleuchtend war.

Zwei weitere Dinge würden wir produktiv nie so lassen: Die Zugangsdaten der Datenbank stehen im Klartext im Repository, damit der Stand für den Kurs reproduzierbar bleibt, und die Images bauen wir lokal und importieren sie in den Cluster, statt eine Registry zu nutzen. Damit hängt der Stand an der Maschine, auf der gebaut wurde.

Und schliesslich die Skalierung: In Dispatch City skaliert nichts von selbst, wir setzen die Replica-Zahl von Hand. Ein HPA nach CPU, wie wir ihn im Lab gesehen haben, würde hier auch wenig helfen. Ein Restaurant-Worker gibt jeder Bestellung eine feste Zubereitungszeit von 900 Millisekunden und rechnet in dieser Zeit nicht; die eigentliche Arbeit danach sind ein paar Mikrosekunden für Dekodieren und Publizieren. Sein Durchsatz liegt damit bei gut einer Bestellung pro Sekunde, während die CPU-Anzeige nahe null bleibt. Die Warteschlange kann also volllaufen, ohne dass ein CPU-Wert je einen Schwellwert erreicht. Aussagekräftig ist hier die Länge der Queue, nicht die Auslastung.

## Was wir im Betrieb erlebt haben

**RabbitMQ wurde nicht ready.** In Block 5 fielen Liveness und Readiness durch, weil der Broker länger zum Starten braucht, als die Probes erlaubten. Wir haben die Werte zuerst direkt im Cluster gepatcht, und beim nächsten Deployment war die Korrektur wieder weg. Erst als die Werte im Overlay standen, blieb es stabil. Das ist im Nachhinein die wichtigste Lektion des Projekts: Was nur im Cluster existiert und nicht im Manifest, ist verloren.

**Nach einem Neustart des Rechners antwortete kubectl nicht mehr.** Wir bekamen nur noch `connection refused` und haben zuerst im Cluster gesucht. Die Nodes liefen aber längst, es fehlte der Loadbalancer-Container von k3d, den Docker beim Herunterfahren beendet hatte. Ein `docker start` auf diesen Container, und alles war wieder da.

**Das Dashboard lieferte 503.** Während ein Node neu startete, standen die Pods auf `Unknown` und die Control API verschwand aus der EndpointSlice. Traefik leitet nur an bereite Ziele weiter und meldete deshalb `503 Service Unavailable`. Sobald der Node wieder `Ready` war, lief es ohne Zutun weiter. Gut zu wissen, bevor man anfängt, an der Konfiguration zu schrauben.

**Eine geänderte ConfigMap tat nichts.** Wir haben den Wert angepasst, angewendet und uns gewundert, dass die Simulation im alten Takt weiterlief. Die Werte werden beim Start des Pods gelesen, es braucht also einen bewussten Neustart.

**In `food.dead` liegen noch Nachrichten aus den Übungen.** Wir haben absichtlich ungültige Events publiziert, um die Dead Letter Queue zu sehen. Dass sie funktioniert, ist schön, aber aufgeräumt oder zurückgespielt haben wir die Nachrichten nie. In einem echten Betrieb bräuchte es dafür einen Ablauf und jemanden, der hinschaut.

## Was wir als Nächstes anders machen würden

Zuerst das Autoscaling: nicht nach CPU, sondern nach der Länge der Queue. Damit würden die Worker bei Stosszeiten von selbst hochfahren, statt dass wir zusehen und nachskalieren.

Dann die Fehlerbehandlung. Heute geht eine Nachricht bei einem fachlichen Fehler sofort in die DLQ. Ein oder zwei Wiederholungen mit Wartezeit wären sinnvoll, damit ein kurzer Ausbruch der Datenbank nicht gleich zu Dauerfehlern führt.

Ausserdem: Backup und Restore einrichten und den Restore einmal wirklich testen, die Images über eine Registry verteilen, die Secrets aus dem Repository nehmen und ein paar fachliche Messwerte ergänzen, etwa die Zeit von der Bestellung bis zur Auslieferung. Technische Metriken haben wir reichlich, fachliche kaum.

## Persönliche Reflexion

### Kilian Hirschi

Mir hat dieses Modul viel Spass gemacht. Von Kubernetes hatte ich vorher schon oft gehört, aber nie wirklich verstanden, was es eigentlich ist. Im Verlauf des Unterrichts konnte ich viel mitnehmen und habe heute das Gefühl, die Grundprinzipien verstanden zu haben.

Schwierig fand ich, über das ganze Projekt hinweg den Überblick zu behalten. Mit jedem Block kamen Komponenten dazu, und nicht bei jeder war mir sofort schlüssig, warum sie an dieser Stelle gebraucht wird. Gleichzeitig war genau das spannend: Es ist eine ganz andere Materie als die, mit der ich sonst zu tun habe.

Die Zusammenarbeit mit Kim war durchgehend angenehm. Wir kannten beide dieses Produkt vorher nicht, und dadurch konnten wir uns gegenseitig helfen, die Grundlagen zu verstehen und sie anschliessend auch anzuwenden.

### Kim Flückiger
