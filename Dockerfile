FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bytestorm ./main

FROM scratch
WORKDIR /app

COPY --from=build /src/bytestorm /app/bytestorm
COPY --from=build /src/config.yaml /app/config.yaml

EXPOSE 8080 9090
ENTRYPOINT ["/app/bytestorm", "-config", "/app/config.yaml"]
