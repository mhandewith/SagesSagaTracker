"""Exercise registration and a container replacement with the same bind mount."""
import json
import os
import pathlib
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

image = os.environ["SMOKE_IMAGE"]
base = "http://127.0.0.1:18080"
name = "saga-smoke"


def docker(*args):
    return subprocess.run(["docker", *args], check=True, capture_output=True, text=True)


def request(path, body=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Authorization"] = "Bearer " + key
    req = urllib.request.Request(base + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=3) as response:
        return json.load(response)


def start(data):
    docker("run", "-d", "--name", name, "-p", "127.0.0.1:18080:8080", "--mount", f"type=bind,source={data},target=/data", image)
    for _ in range(30):
        try:
            request("/healthz")
            return
        except (OSError, urllib.error.URLError):
            time.sleep(1)
    raise RuntimeError("container did not become ready")


with tempfile.TemporaryDirectory() as folder:
    data = pathlib.Path(folder) / "data"
    data.mkdir()
    # Match the image's non-root identity and the documented Unraid setup.
    subprocess.run(["sudo", "chown", "65532:65532", str(data)], check=True)
    try:
        start(data)
        assert request("/api/v1/ping")["message"] == "pong"
        registered = request("/api/v1/guilds", {"name": "Container test guild", "server": "Test Server"})
        key = registered["key"]
        assert request("/api/v1/guilds/me", key=key)["guild"] == registered["guild"]
        docker("exec", name, "/server", "healthcheck")
        docker("stop", name)
        docker("rm", name)
        assert (data / "sagetracker.db").is_file()
        start(data)
        assert request("/api/v1/guilds/me", key=key)["guild"] == registered["guild"]
        try:
            request("/api/v1/guilds/me", key="sgt_" + "a" * 43)
            raise AssertionError("invalid key accepted")
        except urllib.error.HTTPError as error:
            assert error.code == 401
        print("Container registration, key validation, and persistence passed.")
    finally:
        subprocess.run(["docker", "logs", name], check=False)
        subprocess.run(["docker", "rm", "-f", name], check=False, capture_output=True)
        # Restore ownership so TemporaryDirectory can clean up this test's files.
        subprocess.run(["sudo", "chown", "-R", f"{os.getuid()}:{os.getgid()}", str(data)], check=True)
