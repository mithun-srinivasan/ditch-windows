#!/bin/sh
# A TUI needs a terminal on both ends. Keep the automated tour as the default
# for CI and plain `docker run`, and open the full-screen demo for `-it`.
if [ "$#" -eq 0 ]; then
    if [ -t 0 ] && [ -t 1 ]; then
        set -- demo --interactive
    else
        set -- demo
    fi
fi

exec /usr/local/bin/ditch-windows "$@"
