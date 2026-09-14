# Persistent storage and guild registration

This milestone stores guilds in SQLite and issues a guild key that resolves to the guild's friendly name. Group profiles and saga tracking come later.

## Storage behavior

- Container database: `/data/sagetracker.db` (`DATA_DIR=/data`).
- Local `go run ./cmd/server`: `./data/sagetracker.db`, unless `DATA_DIR` is set.
- The server creates the database and applies versioned schema migrations automatically. Startup fails if storage is inaccessible or the schema is newer than this server supports.
- SQLite uses WAL journaling. Keep the entire data directory, including any `sagetracker.db-wal` and `sagetracker.db-shm` files, together.
- `/healthz` now checks database access and returns HTTP 503 if unavailable.
- Guild names are unique ignoring case and repeated/outer whitespace. Names may contain 1–100 characters, with no control characters.
- Keys contain 256 random bits. Only their SHA-256 hashes are stored; the original key is returned once at registration. No recovery or rotation endpoint exists yet. Save it in your password manager before closing the registration session.
- Registration is currently open to anyone who can reach this server. Guild keys grant guild lookup access; they are not server administrator credentials. Keep this milestone on the LAN. Use HTTPS before sending keys across untrusted networks.

## 1. Prepare the Unraid folder

Stop `sages-saga-tracker` in the Docker tab. In the **Unraid terminal**, run:

```sh
mkdir -p /mnt/user/appdata/sages-saga-tracker
chown 65532:65532 /mnt/user/appdata/sages-saga-tracker
chmod 700 /mnt/user/appdata/sages-saga-tracker
```

The image runs as user/group **65532:65532**, so it needs ownership of this directory. These commands affect only this application's directory, not the entire appdata share. This image does not implement `PUID`/`PGID`; adding those environment variables will not change permissions. Do not enable Privileged mode.

Use a local Unraid appdata location, not an SMB/NFS network mount. If your appdata lives at a different local path, substitute that path consistently below.

## 2. Configure the bind mount

Edit the container. Choose **Add another Path, Port, Variable, Label or Device**, select **Path**, and enter:

| Field | Value |
| --- | --- |
| Name | `Appdata` |
| Container Path | `/data` |
| Host Path | `/mnt/user/appdata/sages-saga-tracker` |
| Access Mode | `Read/Write` |

Keep network **Bridge**, host port **8095**, and container port **8080**. `DATA_DIR` is already `/data` in the image; no variable is required. Map the directory, not an individual database file. Click **Apply**.

The mapping makes `/data/sagetracker.db` inside Docker the same file as `/mnt/user/appdata/sages-saga-tracker/sagetracker.db` on Unraid. Removing/replacing a container does not remove that host directory. Without the mapping, container replacement loses its internal data.

## 3. Publish and update

From PowerShell in the repository:

```powershell
cd C:\Repos\SagesSagaTracker
git add README.md docs go.mod go.sum cmd scripts Dockerfile .dockerignore .github/workflows/docker.yml
git commit -m "Add persistent SQLite storage and guild registration"
git push origin main
```

Wait for **Test and publish Docker image** in GitHub Actions. It now creates a guild in a real container, removes the container, starts another with the same bind mount, and validates the original key before publishing.

In Unraid, **Check for Updates** and update `sages-saga-tracker`. Confirm the `/data` mapping is still present. A restart alone does not pull the new image.

## 4. Register your guild

Run in PowerShell on your PC. Replace the example guild name before running:

```powershell
$baseUrl = 'http://192.168.86.127:8095'
Invoke-RestMethod "$baseUrl/healthz"

$registration = Invoke-RestMethod `
    -Method Post `
    -Uri "$baseUrl/api/v1/guilds" `
    -ContentType 'application/json' `
    -Body (@{ name = 'Your Guild Name' } | ConvertTo-Json)

$registration.guild
$guildKey = $registration.key
$guildKey
```

Registration returns **HTTP 201** and JSON containing `guild` (`id`, `name`, `created_at`) and `key`. The key starts with `sgt_`. Copy it to your password manager; do not paste it into GitHub, logs, or chat.

Register only once. Registering the same normalized name again returns **409 Conflict** and does not return or replace the existing key. If a registration response is lost after it reaches the server, the guild may already exist; this initial API has no key recovery flow.

## 5. Validate the key

In the same PowerShell session:

```powershell
$headers = @{ Authorization = "Bearer $guildKey" }
$result = Invoke-RestMethod -Uri "$baseUrl/api/v1/guilds/me" -Headers $headers
$result.guild
```

You should see the same guild ID and friendly name. Keys go in the `Authorization` header, never the URL.

To verify rejection:

```powershell
try {
    Invoke-RestMethod -Uri "$baseUrl/api/v1/guilds/me" `
        -Headers @{ Authorization = 'Bearer invalid-key' }
} catch {
    [int]$_.Exception.Response.StatusCode
}
```

Expected: **401**. Missing keys also return 401. Validation does not return the original key or its stored hash.

## 6. Verify persistence

1. Save your key and note `$result.guild.id`.
2. Restart the container in Unraid and rerun the validation command. The ID should remain unchanged.
3. For a stronger test, edit the container, change a harmless setting such as the WebUI URL, and apply so Unraid recreates the container. Keep the same Appdata mapping. Validate again. The same key must still return the same guild.
4. In the Unraid terminal, confirm the database exists:

```sh
ls -l /mnt/user/appdata/sages-saga-tracker
docker logs sages-saga-tracker
```

The log should report `database ready` with `/data/sagetracker.db`. If you open a new PowerShell session, reconstruct `$baseUrl`, `$guildKey` from your saved key, and `$headers` before testing.

## Backups and restore

For a simple consistent backup, **stop the container**, copy the entire `/mnt/user/appdata/sages-saga-tracker` directory to your backup destination, then start the container. Do not copy only the main `.db` file while the server is running; committed data may still be in the WAL file.

To restore, stop the container, preserve the current directory as a separate backup, replace the complete data directory with the backup, restore ownership to `65532:65532`, and start the container. Never combine database/WAL files from different backups. Restore file and directory permissions so only the container identity and administrators can access the database. Keys issued after the restored backup will no longer work.

## Troubleshooting

- **Permission denied / unable to open database:** confirm the read/write bind mount, folder existence, and ownership `65532:65532`. Existing files restored from elsewhere need the same owner. Change only this app's files.
- **404 on `/api/v1/guilds`:** the old image is likely still running; check `/api/v1/ping` for the new revision and pull the update.
- **401 after recreating a container:** verify the same host directory is mounted at `/data` and that you used the saved key without extra characters.
- **409 registering a name:** that guild already exists. Use its original key; registration does not reset it.
- **Newer database schema:** run an application version that supports that schema or restore its matching backup. An older image cannot automatically undo migrations.

## API summary

| Method/path | Input | Success | Common errors |
| --- | --- | --- | --- |
| `POST /api/v1/guilds` | JSON `{"name":"Your Guild"}` | 201: `guild`, `key` | 400 invalid input; 409 duplicate; 413 too large; 415 wrong content type |
| `GET /api/v1/guilds/me` | `Authorization: Bearer sgt_...` | 200: `guild` | 401 missing/invalid key |
| `GET /healthz` | None | 200: `{"status":"ok"}` | 503 database unavailable |

[Unraid path mapping documentation](https://docs.unraid.net/unraid-os/using-unraid-to/run-docker-containers/managing-and-customizing-containers/) explains how host and container paths correspond. [The SQLite driver documentation](https://pkg.go.dev/modernc.org/sqlite) describes the pure-Go database driver used here.
