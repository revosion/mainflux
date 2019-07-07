FROM erlang:21-alpine AS builder
WORKDIR /mainflux

COPY ./mqtt/* ./
RUN apk add --no-cache git && \
  ./rebar3 compile

FROM vernemq/vernemq:1.8.0-alpine
WORKDIR /mainflux
COPY --from=builder /mainflux .

RUN /vernemq/bin/vmq-admin plugin disable -n vmq_acl && \
  /vernemq/bin/vmq-admin plugin disable -n vmq_passwd && \
  /vernemq/bin/vmq-admin plugin enable -n mfx_auth -p /mainflux/_build/default
