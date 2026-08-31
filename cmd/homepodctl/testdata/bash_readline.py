"""Exercise actual Readline insertion, then inspect an inert command's argv."""

import os
from pathlib import Path
import pty
import select
import shlex
import signal
import sys
import time


shell, script = sys.argv[1:]
directory = Path(script).parent
arguments = directory / "arguments"
prompt = b"R12_READY> "
pid, terminal = pty.fork()
if pid == 0:
    os.chdir(directory)
    os.environ.update(HOME=str(directory), HISTFILE="/dev/null", PS1=prompt.decode())
    os.execv(shell, [shell, "--noprofile", "--norc", "-i"])


def until_prompt():
    output = b""
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if select.select([terminal], [], [], 0.05)[0]:
            output += os.read(terminal, 65536)
            if output.endswith(prompt):
                return output
    raise AssertionError(f"shell prompt timeout: {output!r}")


try:
    until_prompt()
    setup = (
        f"source {shlex.quote(script)}; "
        f"homepodctl() {{ printf '%s\\0' \"$@\" > {shlex.quote(str(arguments))}; }}\n"
    )
    os.write(terminal, setup.encode())
    until_prompt()
    cases = [
        ("volume --value 40 Liv", ["volume", "--value", "40", "Living Room"]),
        ("play --room Liv", ["play", "--room", "Living Room"]),
        ("play --room 'Liv", ["play", "--room", "Living Room"]),
        ('play --room "Liv', ["play", "--room", "Living Room"]),
        ("play --room=Liv", ["play", "--room=Living Room"]),
        ("play --room='Liv", ["play", "--room=Living Room"]),
        ("play --room Dan", ["play", "--room", "Danger$(printf SENTINEL)"]),
        ('play --room "Dan', ["play", "--room", "Danger$(printf SENTINEL)"]),
        ("play --room 'sin", ["play", "--room", "single'quote"]),
        ('play --room "dou', ["play", "--room", 'double"quote']),
        ("play --room bac", ["play", "--room", "back\\slash"]),
    ]
    for line, expected in cases:
        arguments.unlink(missing_ok=True)
        os.write(terminal, ("homepodctl " + line + "\t\n").encode())
        output = until_prompt()
        actual = arguments.read_bytes().decode().split("\0")[:-1]
        assert actual == expected, (line, actual, expected, output)
finally:
    os.kill(pid, signal.SIGKILL)
    os.close(terminal)
    os.waitpid(pid, 0)
