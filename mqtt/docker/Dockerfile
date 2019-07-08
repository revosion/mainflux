FROM erlang:21-alpine AS builder
WORKDIR /mainflux

COPY ./mqtt/* ./
RUN apk add --no-cache git && \
  ./rebar3 compile

FROM vernemq/vernemq:1.8.0-alpine
ENV VMQ_CONF=/etc/vernemq/vernemq.conf
WORKDIR /mainflux
COPY --from=builder /mainflux .

RUN sed -i 's/plugins.vmq_passwd = on/plugins.vmq_passwd = off/' $VMQ_CONF && \
  sed -i 's/plugins.vmq_acl = on/plugins.vmq_acl = off/' $VMQ_CONF && \
  echo 'plugins.mfx_auth = on' >> $VMQ_CONF && \
  echo 'plugins.mfx_auth.path = /mainflux/_build/default' >> $VMQ_CONF
