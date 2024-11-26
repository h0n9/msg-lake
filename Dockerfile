# builder
FROM golang:1.23.3-alpine3.20 AS builder
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY util/ util/
COPY proto/ proto/
COPY cli/ cli/
COPY client/ client/
COPY msg/ msg/
COPY lake/ lake/
COPY relayer/ relayer/
RUN go build ./cmd/msg-lake

# runner
FROM alpine:3.20 AS runner
WORKDIR /usr/bin/app
RUN addgroup --system app && adduser --system --shell /bin/false --ingroup app app
COPY --from=builder /usr/src/app/msg-lake .
RUN chown -R app:app /usr/bin/app
USER app
ENTRYPOINT [ "/usr/bin/app/msg-lake" ]
