# PostgreSQL 16 под управлением Patroni (автоматическое переключение на standby).
FROM postgres:16-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3-venv curl \
    && python3 -m venv /opt/patroni \
    && /opt/patroni/bin/pip install --no-cache-dir 'patroni[etcd3]~=4.0.0' 'psycopg[binary]~=3.2.0' \
    && ln -s /opt/patroni/bin/patronictl /usr/local/bin/patronictl \
    && rm -rf /var/lib/apt/lists/*

COPY patroni.yml /etc/patroni/patroni.yml
COPY post-bootstrap.sh /usr/local/bin/confhub-post-bootstrap.sh
RUN chmod 0755 /usr/local/bin/confhub-post-bootstrap.sh

ENV PATRONICTL_CONFIG_FILE=/etc/patroni/patroni.yml
USER postgres
EXPOSE 5432 8008
ENTRYPOINT ["/opt/patroni/bin/patroni", "/etc/patroni/patroni.yml"]
