# SagesSagaTracker

Minimal Go HTTP service for the future guild saga tracker. This first milestone provides a testable deployment loop: push to GitHub, publish a Docker image, update Unraid, and call the API.

SQLite storage, guild registration, and guild-key validation are now implemented. Follow [the storage and guild setup guide](docs/storage-and-guilds.md) before updating an existing Unraid deployment. Groups and saga tracking are not implemented yet. Registration is currently open to anyone who can reach the service; keep it on your LAN until administrative access controls are added.

## API

| Request | Response |
| --- | --- |
| `GET /` | Service name, version, revision, and endpoint paths |
| `GET /healthz` | `{"status":"ok"}` |
| `GET /api/v1/ping` | `message: pong`, service name, version, Git revision, and UTC time |

The service listens on all interfaces, port **8080** by default. Set the `PORT` environment variable to change its listening port. Docker's built-in health check uses the same variable. Normally leave it at 8080 and change only the host port mapping.

## Run locally

Go 1.26 or newer is required. Docker and CI use Go 1.26. SQLite uses the pure-Go modernc.org/sqlite driver, so no C compiler or external database server is needed. The Go command can download the required toolchain automatically.

In PowerShell:

```powershell
cd C:\Repos\SagesSagaTracker
go run ./cmd/server
```

In another terminal:

```powershell
Invoke-RestMethod http://localhost:8080/api/v1/ping
```

Stop with Ctrl+C. Validate changes with:

```powershell
go test ./...
go vet ./...
```

Optional local Docker test (Docker Desktop must be running in Linux container mode):

```powershell
docker build -t sages-saga-tracker:local .
docker run --rm --name sages-saga-tracker -p 8095:8080 -v saga-local-data:/data sages-saga-tracker:local
```

Call `http://localhost:8095/api/v1/ping`. The image is a static Go binary running as a non-root user and supports graceful shutdown and Docker health checks.

## 1. Push to GitHub

From the repository folder, review the files and commit:

```powershell
git status
git add README.md docs go.mod go.sum cmd scripts Dockerfile .dockerignore .github/workflows/docker.yml
git commit -m "Add Go service and Docker publishing workflow"
git push origin main
```

If Git reports dubious ownership on this specific repository, and this is your own checkout, register only this directory as trusted:

```powershell
git config --global --add safe.directory C:/Repos/SagesSagaTracker
```

Open https://github.com/mhandewith/SagesSagaTracker/actions and wait for **Test and publish Docker image** to succeed. It runs Go tests (including the race detector), vet, builds a Linux amd64 image, checks its HTTP endpoints and health-check command, then publishes to GitHub Container Registry (GHCR). Docker does not need to run on your PC for this workflow.

The workflow uses GitHub's automatic `GITHUB_TOKEN`; you do not need a Docker Hub account or a manually configured registry password. Repository/organization policy must permit GitHub Actions and package publishing. If publishing returns a permissions error, inspect the workflow log and the repository's **Settings > Actions > General** and package access settings.

Main branch pushes publish:

```text
ghcr.io/mhandewith/sagessagatracker:latest
ghcr.io/mhandewith/sagessagatracker:sha-<first-12-commit-characters>
```

Pull requests build and test without publishing. A `v*` tag publishes that exact tag, such as `v0.1.0`, without replacing `latest`. The workflow can also be started manually on main using **Run workflow**.

## 2. Allow Unraid to download the image

After the first successful publish, find **sagessagatracker** under your GitHub account's **Packages** (also linked from the repository if visible). Open **Package settings** and change the package visibility to **Public** if you want Unraid to pull without credentials. A newly published package is private by default, even if the source repository is public. Public image contents are downloadable by anyone.

If you prefer to keep the image private, authenticate the Unraid Docker client to `ghcr.io` using a GitHub personal access token (classic) with `read:packages` and access to the package. Run `docker login ghcr.io -u mhandewith` in the Unraid terminal and enter the token at the password prompt. Do not put a token into a committed file. Public visibility is simpler for this initial deployment.

## 3. Install on Unraid

In Unraid, open **Docker > Add Container**. Configure:

| Field | Value |
| --- | --- |
| Name | `sages-saga-tracker` |
| Repository | `ghcr.io/mhandewith/sagessagatracker:latest` |
| Network Type | `Bridge` |
| Console shell | None needed; the minimal image has no shell |
| Privileged | Off |
| WebUI (advanced view, optional) | `http://[IP]:[PORT:8080]/` |

Choose **Add another Path, Port, Variable, Label or Device**, then add a **Port**:

| Field | Value |
| --- | --- |
| Name | `HTTP` |
| Container Port | `8080` |
| Host Port | `8095` (or another unused port) |
| Connection Type | `TCP` |

Before clicking **Apply**, add the read/write mapping `/mnt/user/appdata/sages-saga-tracker` to `/data` and set its ownership as described in [the storage guide](docs/storage-and-guilds.md). No separate database container is needed. Click **Apply**, wait for the image to download, and enable **Autostart** if desired. Unraid saves the container configuration for later reuse.

## 4. Test your Unraid deployment

Replace `UNRAID_IP` with the server's LAN IP. From your Windows PC:

```powershell
Invoke-RestMethod http://UNRAID_IP:8095/healthz
Invoke-RestMethod http://UNRAID_IP:8095/api/v1/ping
```

Or from the Unraid terminal:

```sh
curl -f http://127.0.0.1:8095/api/v1/ping
```

A browser can open the same URLs. Ping returns JSON like:

```json
{
  "message": "pong",
  "service": "SagesSagaTracker",
  "version": "sha-0123456789ab",
  "revision": "0123456789abcdef...",
  "time": "2026-09-13T23:00:00Z"
}
```

The version, revision, and time vary. After about 30 seconds the container should report healthy. For diagnostics in the Unraid terminal:

```sh
docker logs sages-saga-tracker
docker inspect --format '{{.State.Health.Status}}' sages-saga-tracker
docker exec sages-saga-tracker /server healthcheck
```

If pulling fails with denied/unauthorized, check package visibility or registry login. If the API is unreachable, check that the container is running, that the host port is unused, and that the mapping is host 8095 to container 8080. Use the server's LAN IP when testing from your PC, not localhost.

## 5. Deploy subsequent changes

1. Commit and push changes to `main`.
2. Wait for the GitHub Actions publish workflow to succeed.
3. In Unraid's Docker tab, choose **Check for Updates**, then update this container. A restart alone does not download a new image.
4. Call `/api/v1/ping` and check `revision` against your Git commit.

To roll back, edit the container's Repository field to a previously published `sha-...` tag and apply. Each successful main build has its own commit tag. You can publish a named version with:

```powershell
git tag v0.1.0
git push origin v0.1.0
```

Then use `ghcr.io/mhandewith/sagessagatracker:v0.1.0` in Unraid. Tags beginning with `v` must also be valid Docker tags.

## References

- [GitHub: publishing Docker images](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)
- [GitHub: working with Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Unraid: managing and customizing containers](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/managing-and-customizing-containers/)
