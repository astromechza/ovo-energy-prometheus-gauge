# ovo-energy-prometheus-gauge

A small daemon that fetches energy and gas readings from OVO and makes it available for a prometheus scraper on port `8080`.

**NOTE**: This does _not_ return the estimated usage that OVO anticipates. Instead this uses only the ground truth data
emitted by your smart meter. If your meter is not uploading data, it will be stuck with an old reading and the "age"
metric will go up. Hopefully this will push you to fix your meter or increase its frequency!

Example:

```
# HELP ovo_reading_age_seconds 
# TYPE ovo_reading_age_seconds gauge
ovo_reading_age_seconds{fuel="Electricity",mpxn="2200042999999",msn="19K0099999"} 2.648166863159e+06
ovo_reading_age_seconds{fuel="Gas",mpxn="93703999999",msn="E6S132267999999"} 2.648166780737e+06
# HELP ovo_reading_last 
# TYPE ovo_reading_last gauge
ovo_reading_last{fuel="Electricity",mpxn="22000429999999",msn="19K0099999",tier="anytime"} 11327
ovo_reading_last{fuel="Gas",mpxn="93703999999",msn="E6S132267999999",tier="default"} 5090
```

A config file must be mounted at `/config.json` or another path specified by `-config`. This file must contain the
account number, username, and password. See ([config-example.json](./config-example.json)).

If running in Kubernetes, use a secret to store and mount this file.

By default, this will poll the last readings every 30 minutes.

The image is pushed to Dockerhub. Choose the latest release here: https://hub.docker.com/r/astromechza/ovo-energy-prometheus-gauge/tags.

## Example kubectl apply

First create a secret with your ovo details:

```shell
echo '{"accountNumber": "12345678", "username": "user@example.com", "password": "xxx"}' jq | \
    kubectl create secret generic ovo-energy-account --namespace default --from-file=config.json=/dev/stdin
```

And then apply the following

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ovo-energy-prometheus-gauge
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ovo-energy-prometheus-gauge
  template:
    metadata:
      labels:
        app: ovo-energy-prometheus-gauge
    spec:
      containers:
      - name: ovo-energy-prometheus-gauge
        image: docker.io/astromechza/ovo-energy-prometheus-gauge:v1.0.1
        args: ["-config", "/config/config.json", "-interval", "30m"]
        securityContext:
          runAsNonRoot: true
          runAsUser: 1001
          readOnlyRootFilesystem: true
        ports:
        - name: http
          containerPort: 8080
        readinessProbe:
          httpGet:
              path: /metrics
              port: 8080
        volumeMounts:
          - name: config
            mountPath: /config
            readOnly: true
      volumes:
        - name: config
          secret:
            secretName: ovo-energy-account
```

And a pod-monitor to watch it

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: ovo-energy-prometheus-gauge-monitor
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: ovo-energy-prometheus-gauge
  namespaceSelector:
    matchNames:
      - example
  podMetricsEndpoints:
    - port: http
```