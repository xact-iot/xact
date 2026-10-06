#!/usr/bin/env python3
"""Copy local dashboard definitions without exporting credentials or user data."""
import json
import os
from pathlib import Path
import runpy
import subprocess
from urllib.parse import unquote, urlparse

root = Path(__file__).resolve().parents[1]
dotenv = runpy.run_path(str(root / 'profiling/run-stack.py'))['dotenv']
url = urlparse(dotenv(root / 'server/.env')['DATABASE_URL'])
env = os.environ.copy()
env.update(PGHOST=url.hostname or '', PGPORT=str(url.port or 5432),
           PGDATABASE=url.path.lstrip('/'), PGUSER=unquote(url.username or ''),
           PGPASSWORD=unquote(url.password or ''), PGCONNECT_TIMEOUT='10')
result = subprocess.run(['psql', '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-c',
                         "SELECT coalesce(json_agg(d ORDER BY id),'[]'::json) FROM dashboards d WHERE org_id=1"],
                        env=env, capture_output=True, text=True, check=True)
definitions = json.loads(result.stdout)
destination = root / 'profiling/results/source-dashboards.json'
destination.parent.mkdir(parents=True, exist_ok=True)
destination.write_text(json.dumps(definitions))
destination.chmod(0o600)
print(f'Copied {len(definitions)} dashboard definitions to {destination}')
