#!/usr/bin/env python3
"""Run an isolated XACT/public-bus/Translink stack and capture Go profiles."""
import argparse
import concurrent.futures
import csv
import json
import os
from pathlib import Path
import secrets
import shlex
import signal
import sqlite3
import subprocess
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def dotenv(path):
    values = {}
    if path.exists():
        for line in path.read_text().splitlines():
            if line.strip() and not line.lstrip().startswith('#') and '=' in line:
                key, value = line.split('=', 1)
                parts = shlex.split(value)
                values[key.strip()] = parts[0] if parts else ''
    return values


def clone_bus_config(source, target):
    # Source must be stopped. Immutable mode avoids creating WAL/SHM files next
    # to the original. Runtime tables start empty to measure fresh admission.
    wal = Path(str(source) + '-wal')
    if wal.exists() and wal.stat().st_size:
        raise RuntimeError('Source database has a WAL; stop the app and checkpoint it before cloning')
    src = sqlite3.connect(source.resolve().as_uri() + '?mode=ro&immutable=1', uri=True)
    dst = sqlite3.connect(target)
    tables = list(src.execute("SELECT name, sql FROM sqlite_master WHERE type='table'"))
    for name, sql in tables:
        dst.execute(sql)
        if name in {'schema_migration', 'configuration', 'dataset', 'config_value', 'stop_route_slot'}:
            rows = src.execute(f'SELECT * FROM "{name}"')
            columns = len(rows.description)
            dst.executemany(f'INSERT INTO "{name}" VALUES ({",".join("?" for _ in range(columns))})', rows)
    for (sql,) in src.execute("SELECT sql FROM sqlite_master WHERE type='index' AND sql IS NOT NULL"):
        dst.execute(sql)
    dst.commit()
    src.close()
    dst.close()


def request(url, body=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(url, data=json.dumps(body).encode() if body is not None else None, headers=headers)
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


def profile(output, label, seconds=30):
    try:
        for name, path in [('cpu', f'profile?seconds={seconds}'), ('heap', 'heap'), ('allocs', 'allocs'), ('goroutine', 'goroutine')]:
            with urllib.request.urlopen('http://127.0.0.1:16060/debug/pprof/' + path, timeout=seconds + 30) as response:
                (output / f'{label}-{name}.pprof').write_bytes(response.read())
    except Exception as error:
        print(f'Profile {label}: {error}', flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--fleet-root', type=Path, default=ROOT.parent / 'fleet-tracking')
    parser.add_argument('--run', default=time.strftime('%Y%m%d-%H%M%S'))
    parser.add_argument('--duration', type=int, default=600)
    parser.add_argument('--feed-delay', type=int, default=60)
    parser.add_argument('--static-dir', type=Path, default=ROOT / 'profiling/results/ui')
    parser.add_argument('--rss-limit-mib', type=int, default=5000)
    parser.add_argument('--resume-from', type=Path, help='copy a stopped profiling run for a warm-tree test')
    args = parser.parse_args()
    output = ROOT / 'profiling/results' / args.run
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    secret, password = secrets.token_urlsafe(32), secrets.token_urlsafe(24)
    if args.resume_from:
        previous = json.loads((args.resume_from / 'state.json').read_text())
        password = previous['password']
        for name in ['xact.db', 'public-bus.db']:
            src = sqlite3.connect((args.resume_from / name).resolve().as_uri() + '?mode=ro', uri=True)
            dst = sqlite3.connect(output / name)
            src.backup(dst)
            dst.close()
            src.close()
        # The restored RTDB contains config and explicitly persisted values.
        # Clear only the test app's ACK cache so non-persisted static geometry
        # is republished instead of incorrectly considered already synchronized.
        bus = sqlite3.connect(output / 'public-bus.db')
        bus.execute("UPDATE device_sync_state SET ack_hash='', last_ack_at_ms=0")
        bus.commit()
        bus.close()
    else:
        clone_bus_config(args.fleet_root / 'data/public-bus.db', output / 'public-bus.db')
    env = os.environ.copy()
    env.update(DATABASE_URL='', SQLITE_PATH=str(output / 'xact.db'), PLUGIN_DIR=str(ROOT / 'plugins'),
               STATIC_SERVE_MODE='server', STATIC_DIR=str(args.static_dir.resolve()), API_HOST='127.0.0.1', API_PORT='18080',
               NATS_HOST='127.0.0.1', NATS_PORT='14222', NATS_WS_HOST='127.0.0.1', NATS_WS_PORT='19223',
               NATS_WS_PATH='', NATS_WS_URL='ws://127.0.0.1:19223', NATS_STORE_DIR=str(output / 'nats-store'), NATS_LOG_FILE=str(output / 'nats.log'),
               EMBEDDED_MQTT_SERVER='no', MQTT_CLIENT_ENABLED='no', ENABLE_HTTPS='no',
               JWT_SECRET=secret, NATS_INTERNAL_PASSWORD=secret, PUBLIC_BUS_NATS_PASSWORD=secret,
               TRANSLINK_NATS_PASSWORD=secret, XACT_BOOTSTRAP_ADMIN_PASSWORD=password,
               XACT_PPROF_ADDR='127.0.0.1:16060', GOMEMLIMIT='2GiB')
    processes, logs = {}, []

    def launch(name, child_env):
        log = open(output / f'{name}.log', 'w')
        logs.append(log)
        processes[name] = subprocess.Popen([str(ROOT / 'profiling/results' / name)], cwd=output, env=child_env, stdout=log, stderr=log, start_new_session=True)
        print(f'{name} pid={processes[name].pid}', flush=True)

    def stop(*_):
        for process in reversed(list(processes.values())):
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
        for process in processes.values():
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
        for log in logs:
            log.close()

    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    signal.signal(signal.SIGINT, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    pool = concurrent.futures.ThreadPoolExecutor(max_workers=1)
    try:
        launch('xact', env)
        base = 'http://127.0.0.1:18080'
        for _ in range(60):
            if processes['xact'].poll() is not None:
                raise RuntimeError('XACT exited; see xact.log')
            try:
                request(base + '/xact/health')
                break
            except Exception:
                time.sleep(1)
        auth = request(base + '/xact/login', {'username': 'admin', 'password': password})
        source = ROOT / 'profiling/results/source-dashboards.json'
        definitions = json.loads(source.read_text()) if source.exists() else []
        dashboards = request(base + '/xact/api/v1/dashboards', token=auth['token']) if args.resume_from else []
        for definition in definitions:
            if args.resume_from:
                break
            if definition['org_id'] != 1 or definition['name'] not in {'Buses', 'Buses Configuration', 'Tags Manager'}:
                continue
            body = {k: definition[k] for k in ['name', 'description', 'icon', 'widgets']}
            dashboards.append(request(base + '/xact/api/v1/dashboards', body, auth['token']))
        state = {'url': base + '/xact/', 'password': password, 'output': str(output), 'dashboards': dashboards,
                 'pprof': 'http://127.0.0.1:16060', 'pids': {n: p.pid for n, p in processes.items()}}
        state_path = output / 'state.json'
        state_path.write_text(json.dumps(state))
        state_path.chmod(0o600)
        print(f'Ready: {state["url"]}; state={state_path}', flush=True)
        started = time.monotonic()
        last_ticks, last_time = {}, started
        fleet_env = os.environ.copy()
        fleet_env.update(dotenv(args.fleet_root / '.env'))
        fleet_env.update(NATS_URL='nats://127.0.0.1:14222', NATS_USER='app:public_bus', NATS_PASSWORD=secret,
                         PUBLIC_BUS_DATABASE_URL='sqlite:' + str(output / 'public-bus.db'),
                         XACT_API_URL=base + '/xact', FLEET_LISTEN_ADDR='127.0.0.1:18091',
                         PUBLIC_BUS_IMPORT_DIR=str(output / 'imports'))
        fleet_env.pop('NATS_CREDS_FILE', None)
        phases = set()
        with open(output / 'processes.csv', 'w') as samples:
            writer = csv.writer(samples)
            writer.writerow(['elapsed_s', 'process', 'pid', 'cpu_percent', 'rss_mib'])
            while time.monotonic() - started < args.duration:
                now = time.monotonic()
                elapsed = now - started
                if elapsed >= args.feed_delay and 'public-bus' not in processes:
                    pool.submit(profile, output, 'startup')
                    launch('public-bus', fleet_env)
                if elapsed >= args.feed_delay + 30 and 'translink-adapter' not in processes:
                    adapter_env = fleet_env | {'NATS_USER': 'app:translink'}
                    launch('translink-adapter', adapter_env)
                    state['pids'] = {n: p.pid for n, p in processes.items()}
                    state_path.write_text(json.dumps(state))
                for threshold in [args.feed_delay + 45, args.feed_delay + 120, args.feed_delay + 240]:
                    if elapsed >= threshold and threshold not in phases:
                        phases.add(threshold)
                        pool.submit(profile, output, f'feed-{threshold}')
                rss_total = 0
                for name, process in processes.items():
                    if process.poll() is not None:
                        raise RuntimeError(f'{name} exited ({process.returncode}); see its log')
                    fields = Path(f'/proc/{process.pid}/stat').read_text().split(') ', 1)[1].split()
                    ticks = int(fields[11]) + int(fields[12])
                    cpu = (ticks - last_ticks.get(process.pid, ticks)) / os.sysconf('SC_CLK_TCK') / (now - last_time) * 100
                    rss = int(fields[21]) * os.sysconf('SC_PAGE_SIZE') / 2**20
                    rss_total += rss
                    last_ticks[process.pid] = ticks
                    writer.writerow([round(elapsed, 2), name, process.pid, round(cpu, 2), round(rss, 2)])
                samples.flush()
                if rss_total > args.rss_limit_mib:
                    raise RuntimeError(f'Test RSS budget exceeded: {rss_total:.0f} MiB; stopping isolated stack')
                last_time = now
                time.sleep(2)
    except KeyboardInterrupt:
        print('Stopping isolated profiling stack', flush=True)
    finally:
        stop()
        pool.shutdown(wait=True, cancel_futures=True)


if __name__ == '__main__':
    main()
