# builder
FROM golang:1.20.4-alpine3.17 AS builder
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY util/ util/
COPY proto/ proto/
COPY cli/ cli/
COPY msg/ msg/
COPY lake/ lake/
COPY relayer/ relayer/
RUN go build ./cmd/msg-lake

# runner
FROM alpine:3.17.3 AS runner
WORKDIR /usr/bin/app
RUN addgroup --system app && adduser --system --shell /bin/false --ingroup app app
COPY --from=builder /usr/src/app/msg-lake .
RUN chown -R app:app /usr/bin/app
USER app
ENTRYPOINT [ "/usr/bin/app/msg-lake" ]
