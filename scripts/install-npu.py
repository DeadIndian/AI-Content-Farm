"""Install pinned Intel user-space NPU libraries inside the image only."""
import hashlib
import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import urllib.request

URL = "https://github.com/intel/linux-npu-driver/releases/download/v1.38.0/linux-npu-driver-v1.38.0.20260910-34487311128-ubuntu2404.tar.gz"
SHA256 = "1efcd4b60c22abee751d8f2705962cbcc2a569de45c7e0e670cf08afbfcdc1d2"

with urllib.request.urlopen(URL, timeout=120) as response:
    data = response.read()
if hashlib.sha256(data).hexdigest() != SHA256:
    raise RuntimeError("Intel NPU driver archive checksum mismatch")
with tempfile.TemporaryDirectory() as directory:
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        archive.extractall(directory, filter="data")
    packages = [str(p) for p in Path(directory).rglob("*.deb")
                if p.name.startswith(("intel-level-zero-npu_", "intel-driver-compiler-npu_"))]
    if len(packages) != 2:
        raise RuntimeError("Expected NPU driver and compiler packages")
    subprocess.run(["dpkg", "-i", *packages], check=True)
