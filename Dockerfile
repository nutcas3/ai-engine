FROM golang:1.27.3-alpine AS build
RUN apk add --no-cache curl libstdc++ libgcc

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

ARG TARGETARCH
COPY . .
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1100 && \
    templ generate && \
    case "$TARGETARCH" in amd64) tw_arch=x64 ;; arm64) tw_arch=arm64 ;; *) echo "Unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; esac && \
    curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.4/tailwindcss-linux-$tw_arch-musl" -o tailwindcss && \
    chmod +x tailwindcss && \
    ./tailwindcss -i cmd/web/styles/input.css -o cmd/web/assets/css/output.css

RUN go build -o main ./cmd/api

FROM alpine:3.20.1 AS prod
WORKDIR /app
COPY --from=build /app/main /app/main
EXPOSE ${PORT}
CMD ["./main"]
