#!/usr/bin/env python3
"""Launch a real AppImage under Xvfb; assert both WebKit helpers stay alive.

Run inside a disposable test container with dbus-run-session and DISPLAY set.
The release AppImage is the input, not a server-mode substitute for the GUI.
"""
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time


def descendants(parent):
    processes = {}
    for entry in Path('/proc').glob('[0-9]*'):
        try:
            # comm may contain spaces (and parentheses); fields after it are fixed.
            fields = (entry / 'stat').read_text().rsplit(')', 1)[1].split()
            processes[int(entry.name)] = int(fields[1])
        except (OSError, ValueError, IndexError):
            continue
    found = {parent}
    while True:
        children = {pid for pid, ppid in processes.items() if ppid in found}
        if children <= found:
            return found
        found |= children


def check(image):
    with tempfile.TemporaryDirectory(prefix='cascade smoke ') as temporary:
        root = Path(temporary)
        # Isolate settings, database, and plugins from any existing installation.
        env = dict(os.environ, HOME=str(root), TMPDIR=str(root), XDG_CONFIG_HOME=str(root / 'config'),
                   XDG_DATA_HOME=str(root / 'data'), XDG_CACHE_HOME=str(root / 'cache'))
        with (root / 'launch.log').open('w+') as log:
            proc = subprocess.Popen([str(image), '--appimage-extract-and-run'],
                                    env=env, stdout=log, stderr=log, start_new_session=True)
            try:
                deadline = time.monotonic() + 30
                healthy_since = None
                while time.monotonic() < deadline:
                    if proc.poll() is not None:
                        raise AssertionError(f'AppImage exited early: {proc.returncode}')
                    helpers = set()
                    cascade_pid = None
                    for pid in descendants(proc.pid):
                        try:
                            exe = Path(f'/proc/{pid}/exe').resolve(strict=True)
                            if exe.name in {'WebKitNetworkProcess', 'WebKitWebProcess'}:
                                assert str(exe).startswith(('/usr/lib/', '/usr/lib64/', '/usr/libexec/')), exe
                                helpers.add(exe.name)
                            elif exe.name == 'cascade':
                                cascade_pid = pid
                                maps = Path(f'/proc/{pid}/maps').read_text()
                                for library in ('libwebkitgtk-6.0', 'libjavascriptcoregtk-6.0', 'libgtk-4'):
                                    paths = {line.split()[-1] for line in maps.splitlines() if library in line}
                                    assert paths and all(p.startswith(('/usr/lib/', '/usr/lib64/')) for p in paths), paths
                        except FileNotFoundError:
                            continue
                    window = subprocess.run(
                        ['xdotool', 'search', '--onlyvisible', '--pid', str(cascade_pid or 0), '--name', 'Cascade Chat'],
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
                    if len(helpers) == 2 and window:
                        healthy_since = healthy_since or time.monotonic()
                        if time.monotonic() - healthy_since >= 5:
                            print('PASS: Cascade window and both host WebKit helpers survived for 5 seconds')
                            return
                    else:
                        healthy_since = None
                    time.sleep(0.2)
                raise AssertionError('Timed out waiting for Cascade window and both WebKit helpers')
            except BaseException:
                log.flush()
                log.seek(0)
                print(log.read(), file=sys.stderr)
                raise
            finally:
                try:
                    os.killpg(proc.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait()


if __name__ == '__main__':
    check(Path(sys.argv[1]).resolve(strict=True))
