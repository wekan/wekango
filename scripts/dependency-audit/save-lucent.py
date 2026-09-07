#!/usr/bin/env python3
"""Preserve the reviewed Lucent permissive notice despite scanner type gap."""
import hashlib
from pathlib import Path
import sys

source, destination = map(Path, sys.argv[1:])
content = source.read_bytes()
expected = 'eb4df27f95a096a23d88e567331a4bee5590d22185942a1b0456197e29d03e59'
if hashlib.sha256(content).hexdigest() != expected:
    raise SystemExit('Lucent notice changed: review its terms before updating the approved digest')
destination.parent.mkdir(parents=True, exist_ok=True)
destination.write_bytes(content)
