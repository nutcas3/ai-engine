FROM golang:1.24.4-alpine AS build
RUN apk add --no-cache curl libstdc++ libgcc

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

ARG TARGETARCH
COPY . .
RUN go install github.com/a-h/templ/cmd/templ@latest && \
    templ generate && \
    arch=$${TARGETARCH:-amd64}; [ "$$arch" = "amd64" ] && arch=x64; \
    curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-$$arch-musl" -o tailwindcss && \
    chmod +x tailwindcss && \
    ./tailwindcss -i cmd/web/styles/input.css -o cmd/web/assets/css/output.css

RUN go build -o main cmd/api/main.go

FROM alpine:3.20.1 AS prod
WORKDIR /app
COPY --from=build /app/main /app/main
EXPOSE ${PORT}
CMD ["./main"]


