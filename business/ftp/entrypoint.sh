#!/bin/sh
set -eu
umask 077
state=/var/lib/sftpgo
data=/srv/sftpgo/data
identity=/addp/config/identity.sha256
test -r "$identity"
test -d "$data" && test -r "$data" && test -x "$data" || {
    echo 'FTP account root must be an existing readable directory.' >&2
    exit 1
}
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
exec sftpgo serve --config-dir /addp/config --log-file-path '' \
    --loaddata-from /addp/config/users.json --loaddata-mode 1
