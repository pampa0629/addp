"""Exercise the real node recovery transaction on a disposable limited source."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import tempfile
import time
import urllib.request
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('node_recovery_t2',ROOT/'scripts/infra/node-recovery.py')
M=importlib.util.module_from_spec(spec);spec.loader.exec_module(M)


def main():
    project='addp-node-recovery-t2-'+secrets.token_hex(6)
    docker=M.deployment.Docker(os.environ)
    with tempfile.TemporaryDirectory(prefix='addp-node-recovery-t2-') as folder:
        work=Path(folder).resolve(); tls=work/'tls';tls.mkdir()
        def openssl(*args):
            subprocess.run(['openssl',*args],cwd=tls,check=True,capture_output=True,timeout=15,
                           env=dict(os.environ,OPENSSL_CONF=os.devnull))
        openssl('req','-x509','-newkey','rsa:2048','-nodes','-subj','/CN=Disposable Node CA','-days','1',
                '-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign',
                '-addext','subjectKeyIdentifier=hash','-keyout','ca.key','-out','ca.crt')
        for name,usage in (('server','serverAuth'),('client','clientAuth')):
            openssl('req','-newkey','rsa:2048','-nodes','-subj','/CN=Disposable Node '+name,'-keyout',name+'.key','-out',name+'.csr')
            (tls/'extensions').write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\nextendedKeyUsage='+usage+'\nsubjectAltName=IP:127.0.0.1\n')
            openssl('x509','-req','-in',name+'.csr','-CA','ca.crt','-CAkey','ca.key','-CAcreateserial','-days','1','-extfile','extensions','-out',name+'.crt')
        (tls/'server.key').chmod(0o644)
        files=[ROOT/'scripts/infra'/name for name in ('node-metrics.yml','node-metrics-desktop.yml','node-block-capacity.yml')]
        config={**docker.env,'ADDP_NODE_METRICS_ENABLED':'true','ADDP_NODE_METRICS_TLS_DIR':str(tls),
                'ADDP_NODE_METRICS_OWNER':str(ROOT),'ADDP_NODE_METRICS_LISTEN':':9100',
                'ADDP_NODE_METRICS_ROOTFS':'/','ADDP_NODE_METRICS_PROCFS':'/proc','ADDP_NODE_METRICS_SYSFS':'/sys',
                'ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX':'--no-collector',
                'ADDP_NODE_METRICS_TEXTFILE':'--collector.textfile','ADDP_NODE_METRICS_PUBLISH_IP':'127.0.0.1',
                'ADDP_NODE_METRICS_PUBLISH_PORT':'0','ADDP_NODE_CAPACITY_SYSFS':'/sys','ADDP_NODE_CAPACITY_PROCFS':'/proc',
                'ADDP_NODE_METRICS_KERNEL':json.loads(docker.run(['info','--format','{{json .KernelVersion}}']))}
        flags=['compose','--env-file','/dev/null','-p',project,*[arg for path in files for arg in ('-f',str(path))]]
        def compose(*args):return docker.run([*flags,*args],extra_env=config,timeout=90)
        def inputs(env,transport):
            # This fixture deliberately uses bridge namespaces on Linux too.
            # The product provider's native/Desktop signature is tested in T1.
            paths=[*files,ROOT/'scripts/infra/node-metrics-web.yml',ROOT/'scripts/infra/block-capacity.py',*[tls/name for name in ('ca.crt','server.crt','server.key')]]
            def fingerprint(path):
                stat=path.stat()
                return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),
                        'metadata':[stat.st_dev,stat.st_ino,stat.st_size,stat.st_mtime_ns,stat.st_ctime_ns,stat.st_mode,stat.st_uid,stat.st_gid]}
            return {'capacity':True,'engine':json.loads(transport.run(['info','--format','{{json .ID}}'])),
                    'kernel':config['ADDP_NODE_METRICS_KERNEL'],
                    'templates':{p.name:fingerprint(p) for p in paths if p.parent!=tls},
                    'tls':{p.name:fingerprint(p) for p in paths if p.parent==tls}}
        def inspect():return M.containers(docker,project)
        def expect_rejected(operation, reason=None):
            try:operation()
            except ValueError as error:
                if reason is not None:assert getattr(error,'node_recovery_reason',None)==reason
                return
            raise AssertionError('Node recovery accepted unproven installation')
        def collected():
            port=json.loads(docker.run(['inspect','--format','{{json .NetworkSettings.Ports}}',inspect()['node-exporter']['id']]))['9100/tcp'][0]['HostPort']
            context=ssl.create_default_context(cafile=str(tls/'ca.crt'))
            context.load_cert_chain(str(tls/'client.crt'),str(tls/'client.key'))
            deadline=time.monotonic()+20
            while True:
                try:
                    with urllib.request.urlopen('https://127.0.0.1:'+port+'/metrics',context=context,timeout=3) as response:
                        body=response.read(2<<20)
                    assert b'node_memory_MemTotal_bytes' in body and b'addp_block_capacity_success 1' in body
                    return
                except (OSError,AssertionError):
                    if time.monotonic()>=deadline:raise
                    time.sleep(.5)
        try:
            with patch.object(M,'state_directory',return_value=work/'proof'):
                def manage(action,op=lambda:None):M.manage(action,config,project,inputs,op)
                expect_rejected(lambda:manage('recover'), 'node-proof-missing')
                manage('up',lambda:compose('up','-d','--wait','--wait-timeout','30','node-exporter','block-capacity'))
                facts=inspect(); original=M.stable(facts)
                for name,fact in facts.items():
                    host=fact['host'];assert host['ReadonlyRootfs'] and not host['Privileged'] and host['PidMode']!='host'
                    assert fact['user']=='65534:65534' and host['CapDrop']==['ALL']
                    assert host['NetworkMode']!='host'
                    allowed={'/sampler.py','/textfile'} if name=='block-capacity' else {'/etc/addp/node-metrics/web.yml','/etc/addp/node-metrics/tls/ca.crt','/etc/addp/node-metrics/tls/server.crt','/etc/addp/node-metrics/tls/server.key','/textfile'}
                    assert {m['Destination'] for m in fact['mounts']}==allowed
                    assert all(not m['RW'] for m in fact['mounts'] if name!='block-capacity' or m['Destination']!='/textfile')
                collected(); proof=work/'proof/installation.json'
                # Simulate a prior mount's device numbers only; never remount the
                # host or change source files, container identities or security.
                prior=json.loads(proof.read_text())
                for group in ('templates','tls'):
                    for item in prior['inputs'][group].values():item['metadata'][0]+=1
                M.write(proof,prior);metadata=proof.stat();persisted=proof.read_bytes()
                manage('recover');assert inspect()==facts
                assert proof.read_bytes()==persisted
                assert proof.stat().st_ino==metadata.st_ino and proof.stat().st_mtime_ns==metadata.st_mtime_ns
                compose('stop','block-capacity');manage('recover');assert inspect()['node-exporter']==facts['node-exporter']
                collected()
                # The start takes effect, but its reply is lost. Exercise the
                # real cleanup path and preserve the previously healthy source.
                compose('stop','block-capacity')
                start_id=inspect()['block-capacity']['id']; send=M.deployment.Docker.run
                def lost_reply(transport,args,**options):
                    result=send(transport,args,**options)
                    if args==['start',start_id]:raise ValueError('disposable lost start reply')
                    return result
                with patch.object(M.deployment.Docker,'run',lost_reply):
                    expect_rejected(lambda:manage('recover'))
                assert not inspect()['block-capacity']['running']
                assert inspect()['node-exporter']==facts['node-exporter']
                manage('recover');collected()
                compose('stop','node-exporter','block-capacity');manage('recover')
                assert M.stable(inspect())==original;assert all(x['running'] for x in inspect().values());collected()
                # Drift is read-only; neither an existing healthy source nor its volume is replaced.
                prior=(tls/'server.crt').read_bytes();(tls/'server.crt').write_bytes(prior+b'\n')
                before=inspect();expect_rejected(lambda:manage('recover'), 'node-inputs-changed');assert inspect()==before
                (tls/'server.crt').write_bytes(prior)
                volume=M.volume(docker,project)
                compose('rm','-f','-s','block-capacity');expect_rejected(lambda:manage('recover'), 'node-source-incomplete')
                assert M.volume(docker,project)==volume
                assert inspect()=={'node-exporter':before['node-exporter']}
                manage('down');expect_rejected(lambda:manage('recover'), 'node-proof-disabled')
                print('Node recovery T2: prior device numbers preserve healthy source/proof, sampler-only/full stop recovery, lost-reply cleanup, exact identities, mTLS, drift/missing-source/retired-proof rejection PASS')
        finally:
            compose('down','--volumes','--remove-orphans')
            for kind in ('container','network','volume'):
                args=[kind,'ls',*(['-a'] if kind=='container' else []),'-q','--filter','label=com.docker.compose.project='+project]
                assert not docker.run(args).strip(), 'Node recovery T2 residue: '+kind
            print('Node recovery T2 cleanup: containers/networks/volumes = 0')
    assert not work.exists()
    print('Node recovery T2 cleanup: private state = 0')


if __name__=='__main__':main()
