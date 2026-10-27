"""A slow job. It finishes about 90 seconds after it starts and writes a
token to result.txt that cannot be known before it runs."""

import secrets
import time
from pathlib import Path

time.sleep(90)
Path("result.txt").write_text("job-" + secrets.token_hex(8) + "\n")
print("done")
