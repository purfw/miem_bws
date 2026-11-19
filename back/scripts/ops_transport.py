import http.client
import socket
import ssl
import subprocess
import urllib.request
from pathlib import Path

SERVICES = {"identity-svc", "conference-svc", "application-svc", "engagement-svc"}


def internal_opener(root: Path, project="confhub"):
    certificates = root / "secrets" / "mtls"
    context = ssl.create_default_context(cafile=str(certificates / "ca.pem"))
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(
        str(certificates / "operations" / "cert.pem"),
        str(certificates / "operations" / "key.pem"),
    )

    class DockerConnection(http.client.HTTPSConnection):
        def connect(self):
            if self.host not in SERVICES:
                raise OSError("Unknown internal service")
            address = subprocess.check_output(
                [
                    "docker",
                    "inspect",
                    "-f",
                    f'{{{{(index .NetworkSettings.Networks "{project}_app").IPAddress}}}}',
                    f"{project}-{self.host}-1",
                ],
                text=True,
            ).strip()
            connection = socket.create_connection((address, self.port), self.timeout)
            try:
                self.sock = self._context.wrap_socket(
                    connection, server_hostname=self.host
                )
            except BaseException:
                connection.close()
                raise

    class DockerHTTPSHandler(urllib.request.HTTPSHandler):
        def https_open(self, request):
            return self.do_open(DockerConnection, request, context=self._context)

    return urllib.request.build_opener(
        urllib.request.ProxyHandler({}), DockerHTTPSHandler(context=context)
    )
