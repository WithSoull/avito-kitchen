# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build

ARG BINARY
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${BINARY}

FROM alpine:3.22

RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/service /app/service

USER app
EXPOSE 8080 8090
ENTRYPOINT ["/app/service"]
