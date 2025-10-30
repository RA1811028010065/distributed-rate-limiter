# Tooling Requirements

This guide enumerates the software required to build, test, and operate the distributed rate limiter. Where possible it links to platform-specific installation commands so you can bootstrap a workstation quickly.

## Required tools

| Purpose | Tool | Notes |
| --- | --- | --- |
| Build & unit tests | [Go 1.21+](https://go.dev/dl/) | Ensure `go env GOMODCACHE` points to a location with enough space. |
| Container builds & Compose stack | [Docker Engine](https://docs.docker.com/engine/install/) | The Compose plugin (`docker compose`) ships with recent Docker Desktop / Engine releases. |
| Kubernetes deployment | [kubectl](https://kubernetes.io/docs/tasks/tools/) | Required for applying manifests and inspecting cluster state. |
| Local Kubernetes cluster | [Kind](https://kind.sigs.k8s.io/) | Used by the CI pipeline and the manual smoke tests. |
| Scripting | GNU Make | Preinstalled on macOS and most Linux distributions; on Windows install via Chocolatey or Git for Windows. |
| JSON formatting | [`jq`](https://stedolan.github.io/jq/download/) | Optional but recommended for inspecting REST responses. |

## Linux installation commands

```bash
# Ubuntu / Debian
sudo apt update
sudo apt install -y golang jq make docker-compose-plugin
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"
curl -Lo ./kubectl https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
GO_VERSION=1.21.5
curl -LO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
rm go${GO_VERSION}.linux-amd64.tar.gz
curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x kind
sudo mv kind /usr/local/bin/
```

The `docker-compose-plugin` package installed above provides the `docker compose` sub-command. If you prefer the standalone
binary, install it via `sudo apt install docker-compose` and the Makefile targets will detect it automatically.

Log out and back in (or `newgrp docker`) so Docker permissions take effect.

## macOS installation commands

```bash
# Install Homebrew if missing
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Core tooling
brew install go jq kind kubernetes-cli

# Docker Desktop (includes docker compose)
brew install --cask docker
open -a Docker
```

Verify Go and Docker after installation:

```bash
go version
docker version
```

## Windows 11 (PowerShell) installation commands

```powershell
winget install -e --id GoLang.Go
winget install -e --id Docker.DockerDesktop
winget install -e --id Kubernetes.kubectl
winget install -e --id Kubernetes.kind
winget install -e --id jqlang.jq
winget install -e --id GnuWin32.Make
```

Launch Docker Desktop once so it finalises the configuration. The Make binary installs under `C:\Program Files (x86)\GnuWin32\bin`; add it to your `PATH`.

## Validation checklist

After installing the tooling, run the following to confirm everything is wired correctly:

```bash
go version
docker compose version
kubectl version --client
kind version
make --version
```

If you plan to run the Kubernetes smoke test locally, also verify that a Kind cluster can be created:

```bash
kind create cluster --name validate --wait 60s
kubectl get nodes
kind delete cluster --name validate
```

Remember to log out and back in if you added your user to the Docker group on Linux.

## Docker networking considerations

The Docker Compose stack allocates a dedicated bridge network called `ratelimiter_net` and defaults to the `172.31.255.0/28` subnet so it does not conflict with typical corporate address plans. Export `RATE_LIMITER_NETWORK` or `RATE_LIMITER_SUBNET` before running `make compose-up` if your host already uses that range or if you prefer to pin the stack to a different segment.
