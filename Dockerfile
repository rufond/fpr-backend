FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fpr ./cmd/fpr

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S fpr \
    && adduser -S -G fpr fpr

WORKDIR /app

COPY --from=build /out/fpr /app/fpr
COPY database/migrations /app/database/migrations

USER fpr

EXPOSE 8080

ENTRYPOINT ["/app/fpr"]
