FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/kafka-producer-sidecar ./cmd/kafka-producer-sidecar

FROM alpine:3.22
COPY --from=build /out/kafka-producer-sidecar /usr/local/bin/kafka-producer-sidecar
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/kafka-producer-sidecar"]
