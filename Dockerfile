#docker build -t go-worker-transfer .

FROM golang:1.21 As builder

WORKDIR /app
COPY . .

WORKDIR /app/cmd
RUN go build -o go-worker-transfer -ldflags '-linkmode external -w -extldflags "-static"'

FROM alpine

WORKDIR /app
COPY --from=builder /app/cmd/go-worker-transfer .

CMD ["/app/go-worker-transfer"]