FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go build -o /out/webhook-go .

FROM alpine:3.20
RUN adduser -D -g '' appuser
WORKDIR /app
COPY --from=build /out/webhook-go /usr/local/bin/webhook-go
USER appuser
EXPOSE 8080
ENTRYPOINT ["webhook-go"]
