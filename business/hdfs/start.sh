#!/usr/bin/env bash
# Shared Hadoop 3.5 configuration for Business and owned T2 deployments.
set -euo pipefail
role=${1:?namenode or datanode required}
case "$role" in namenode|datanode) ;; *) exit 2;; esac
export HADOOP_CONF_DIR=/opt/hadoop/etc/hadoop
export HADOOP_HEAPSIZE_MAX=512
python3 - <<'PY'
import os
from urllib.parse import urlsplit
import xml.etree.ElementTree as ET
from pathlib import Path
def write(name, values):
    root = ET.Element('configuration')
    for key, value in values.items():
        node = ET.SubElement(root, 'property')
        ET.SubElement(node, 'name').text = key
        ET.SubElement(node, 'value').text = str(value)
    ET.ElementTree(root).write(Path(os.environ['HADOOP_CONF_DIR']) / name, encoding='utf-8', xml_declaration=True)
write('core-site.xml', {'fs.defaultFS': os.environ['HDFS_RPC_URI'], 'hadoop.security.authentication': 'simple'})
write('hdfs-site.xml', {
    'dfs.namenode.rpc-address': urlsplit(os.environ['HDFS_RPC_URI']).netloc,
    'dfs.namenode.rpc-bind-host': '0.0.0.0', 'dfs.namenode.http-address': '0.0.0.0:9870',
    'dfs.namenode.name.dir': '/data/name', 'dfs.datanode.data.dir': '/data/data',
    'dfs.replication': 1, 'dfs.webhdfs.enabled': 'true', 'dfs.permissions.enabled': 'true',
    'dfs.ls.limit': int(os.environ.get('HDFS_LIST_LIMIT', '1000')),
    'dfs.datanode.hostname': os.environ['HDFS_DATANODE_HOST'],
    'dfs.datanode.address': '0.0.0.0:' + os.environ.get('HDFS_DATA_PORT', '9866'),
    'dfs.datanode.http.address': '0.0.0.0:' + os.environ.get('HDFS_DATA_HTTP_PORT', '9864'),
    'dfs.datanode.ipc.address': '0.0.0.0:9867', 'dfs.client.use.datanode.hostname': 'true',
    'dfs.namenode.datanode.registration.ip-hostname-check': 'false',
})
PY
if [ "$role" = namenode ] && [ ! -f /data/name/current/VERSION ]; then
    mkdir -p /data/name
    if [ -n "$(ls -A /data/name)" ]; then
        echo "Refusing to format nonempty HDFS NameNode storage" >&2; exit 1
    fi
    hdfs namenode -format -nonInteractive
fi
exec hdfs "$role"
