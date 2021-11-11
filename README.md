# retry-proxy

Reverse proxy with retry capabilities.

1. Retries requests with linear backoff.
2. Continues broken transfers with sequential [range requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests).

This proxy was made to withstand connection interruptions of user downloads during rollouts inside k8s cluster.

Usage:

```
./retry-proxy
NAME:
   retry-proxy - Retries requests

USAGE:
   retry-proxy [global options] command [command options] [arguments...]

VERSION:
   0.0.1

COMMANDS:
   serve, s  Serves web server
   help, h   Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help
   --version, -v  print the version
```
