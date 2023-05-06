FROM golang:1.20 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /ovo-energy-prometheus-gauge

FROM scratch
USER 1001
COPY --from=builder /ovo-energy-prometheus-gauge /
ENTRYPOINT ["/ovo-energy-prometheus-gauge"]
