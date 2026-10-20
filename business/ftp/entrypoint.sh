#!/bin/sh
set -eu
umask 077
state=/var/lib/sftpgo
data=/srv/sftpgo/data/examples
identity=/addp/config/identity.sha256
test -r "$identity"
if [ -f "$state/configuration.identity" ]; then
    cmp -s "$identity" "$state/configuration.identity" || {
        echo 'FTP account configuration differs from its persistent state; refusing implicit replacement.' >&2
        exit 1
    }
elif [ -e "$state/sftpgo.db" ]; then
    echo 'FTP database has no owner configuration marker; refusing to initialize existing state.' >&2
    exit 1
else
    cp "$identity" "$state/configuration.identity.tmp"
    mv "$state/configuration.identity.tmp" "$state/configuration.identity"
fi
mkdir -p "$data"
for source in /addp/samples/*; do
    target="$data/${source##*/}"
    if [ -e "$target" ] || [ -L "$target" ]; then
        test -f "$target" && test ! -L "$target" || {
            echo 'FTP sample target is not an ordinary file; refusing replacement.' >&2
            exit 1
        }
    else
        # Noclobber prevents replacement even if the target appears mid-start.
        (set -C; cat "$source" > "$target")
    fi
done
exec sftpgo serve --config-dir /addp/config --log-file-path '' \
    --loaddata-from /addp/config/users.json --loaddata-mode 1
