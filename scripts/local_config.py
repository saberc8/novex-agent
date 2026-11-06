"""Read local configuration without shell evaluation or printing secrets."""
from pathlib import Path
import os

ROOT = Path(__file__).resolve().parents[1]


def configuration():
    values = {}
    path = ROOT / '.env.local'
    if path.exists():
        for line in path.read_text().splitlines():
            if line.strip() and not line.lstrip().startswith('#') and '=' in line:
                name, value = line.split('=', 1)
                values[name.strip()] = value
    values.update(os.environ)
    return values
