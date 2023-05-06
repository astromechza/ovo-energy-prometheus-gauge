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

You can use the Dockerfile to produce a container image: `docker build .` but an initial release of this has been pushed
to `docker.io/astromechza/ovo-energy-prometheus-gauge:4cca4fa`.
