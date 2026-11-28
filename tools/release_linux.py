#!/usr/bin/env python3
"""Build the Linux amd64/arm64 Web/CLI archive; never upload or mutate source."""
import argparse
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
MACHINES = {"amd64": 62, "arm64": 183}


def validate_archive(path):
    """Refuse wrong architecture, links, extra entries or lost executable mode."""
    match = re.fullmatch(r'zwai-\d+\.\d+\.\d+-linux-(amd64|arm64)\.tar\.gz', Path(path).name)
    if not match:
        raise ValueError('Linux archive name must identify its version and supported architecture')
    arch = match.group(1)
    try:
        with tarfile.open(path, 'r:gz') as archive:
            entries = archive.getmembers()
            if (len(entries) != 1 or entries[0].name != 'zwai'
                    or not entries[0].isfile() or entries[0].mode != 0o755):
                raise ValueError('Linux archive must contain only executable zwai (0755)')
            with archive.extractfile(entries[0]) as stream:
                header = stream.read(64)
            if (len(header) != 64 or header[:7] != b'\x7fELF\x02\x01\x01'
                    or int.from_bytes(header[16:18], 'little') not in (2, 3)
                    or int.from_bytes(header[18:20], 'little') != MACHINES[arch]):
                raise ValueError(f'Linux archive must contain a 64-bit little-endian {arch} ELF executable')
    except (tarfile.TarError, EOFError) as error:
        raise ValueError('Linux installer is not a valid tar.gz') from error


def build(root, version, output, arch="arm64"):
    if not re.fullmatch(r'\d+\.\d+\.\d+', version):
        raise ValueError('Linux release requires major.minor.patch')
    if arch not in MACHINES:
        raise ValueError('Unsupported Linux architecture')
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    name = f'zwai-{version}-linux-{arch}.tar.gz'
    # Stage on the destination filesystem so replacement is atomic. A failed
    # compiler/validation leaves any existing accepted archive untouched.
    with tempfile.TemporaryDirectory(prefix='.linux-release-', dir=output) as directory:
        stage = Path(directory)
        exe = stage / 'zwai'
        subprocess.run(['go', 'build', '-tags', 'server', '-ldflags',
                        f'-X main.version={version} -X main.linuxArchiveBuild=1',
                        '-o', str(exe.resolve()), './cmd/zwai'],
                       cwd=root, env=dict(os.environ, GOOS='linux', GOARCH=arch, CGO_ENABLED='0'),
                       check=True)
        exe.chmod(0o755)
        path = stage / name
        with tarfile.open(path, 'w:gz') as archive:
            archive.add(exe, arcname='zwai')
        validate_archive(path)
        destination = output / name
        path.replace(destination)
    return destination


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'bin')
    parser.add_argument('--arch', nargs='+', choices=MACHINES, default=['arm64'])
    args = parser.parse_args()
    for arch in dict.fromkeys(args.arch):
        print(build(ROOT, args.version, args.output.resolve(), arch))
