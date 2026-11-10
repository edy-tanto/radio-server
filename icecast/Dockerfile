FROM alpine:3.20
RUN apk add --no-cache icecast mailcap && mkdir -p /var/log/icecast && \
    ln -sf /dev/stdout /var/log/icecast/access.log && ln -sf /dev/stderr /var/log/icecast/error.log && \
    chown -R icecast:icecast /var/log/icecast
COPY icecast.xml /icecast.xml.tpl
USER icecast
EXPOSE 8000
CMD sed -e "s|@SOURCE_PASSWORD@|$SOURCE_PASSWORD|" -e "s|@ADMIN_PASSWORD@|$ADMIN_PASSWORD|" \
        -e "s|@HOSTNAME@|$ICECAST_HOSTNAME|" /icecast.xml.tpl > /tmp/icecast.xml && exec icecast -c /tmp/icecast.xml
