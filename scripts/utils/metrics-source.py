"""Fixed private deployment-source transport, image and container ownership."""
import base64
import hashlib
import importlib.util
import io
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

spec=importlib.util.spec_from_file_location('metrics_source_private',Path(__file__).with_name('private-state.py'))
private=importlib.util.module_from_spec(spec);spec.loader.exec_module(private)

def decode(value): return json.loads(value)
def write(path,value):
    previous=private.read(path,0o600)[0] if os.path.lexists(path) else None
    private.atomic(path,value,0o600,previous=previous)

def transport(env, root, prefix, error=ValueError):
    Error = error
    match = re.fullmatch(r'(\[[0-9a-f:]+\]|[0-9.]+):([1-9][0-9]{0,4})', env.get(prefix+'_LISTEN', ''))
    if not match:
        raise Error('Metrics source requires an explicit private IP and port')
    ip, port = ipaddress.ip_address(match[1].strip('[]')), int(match[2])
    if port > 65535 or ip.is_unspecified or ip.is_multicast or ip.is_link_local or (ip.version == 6 and ip.ipv4_mapped):
        raise Error('Metrics source publication rejected')
    networks = tuple(ipaddress.ip_network(n) for n in ('127.0.0.0/8','::1/128','10.0.0.0/8','172.16.0.0/12','192.168.0.0/16','fc00::/7'))
    if not any(ip.version == n.version and ip in n for n in networks):
        raise Error('Metrics source publication rejected')
    if env[prefix+'_LISTEN'] != (f'[{ip}]:{port}' if ip.version == 6 else f'{ip}:{port}'):
        raise Error('Metrics source publication is not canonical')
    directory = Path(env.get(prefix+'_TLS_DIR', ''))
    if not directory.is_absolute() or directory.resolve().is_relative_to(root) or directory.is_symlink():
        raise Error('Metrics source TLS requires an external directory')
    material = {}
    for name in ('ca.crt', 'server.crt', 'server.key'):
        path = directory/name
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 65536 or path.stat().st_nlink != 1:
            raise Error('Metrics source TLS material rejected')
        material[name] = path.read_bytes()
        if not material[name]:
            raise Error('Metrics source TLS material rejected')
    if (directory/'server.key').stat().st_uid != os.getuid() or (directory/'server.key').stat().st_mode & 0o077:
        raise Error('Metrics source private key ownership rejected')
    try:
        subprocess.run(['openssl', 'verify', '-CAfile', str(directory/'ca.crt'), '-purpose', 'sslserver', '-verify_ip', str(ip), str(directory/'server.crt')], check=True, capture_output=True, timeout=5)
        public = subprocess.run(['openssl', 'x509', '-in', str(directory/'server.crt'), '-pubkey', '-noout'], check=True, capture_output=True, timeout=5).stdout
        key = subprocess.run(['openssl', 'pkey', '-in', str(directory/'server.key'), '-passin', 'pass:', '-pubout'], check=True, capture_output=True, timeout=5).stdout
        if public != key:
            raise Error('Metrics source certificate key mismatch')
    except (OSError, subprocess.SubprocessError):
        raise Error('Metrics source TLS verification failed') from None
    return str(ip), port, material


class Source:
    def __init__(self,docker,scope,state,root,kind,project,error=ValueError):
        if kind not in {'redis','minio'} or scope not in {'infra','business'}: raise error('Unknown metrics source owner')
        self.docker,self.scope,self.state,self.root,self.kind,self.project,self.error=docker,scope,Path(state),Path(root),kind,project,error
        self.namespace='io.addp.'+kind+'-metrics'
        self.owner,self.scope_label=self.namespace+'.owner',self.namespace+'.scope'
        self.service=kind+'-metrics'
        self.config_volume=project+'-private'

    def stop(self):
        ROOT, OWNER, SCOPE, Error = self.root, self.owner, self.scope_label, self.error
        identities = self.docker.run(['ps', '-aq', '--filter', 'label=com.docker.compose.project='+self.project]).split()
        for identity in identities:
            labels = self.docker.fact(identity)['labels']
            if labels.get(OWNER) != str(ROOT) or labels.get(SCOPE) != self.scope or labels.get('com.docker.compose.service') not in (self.service, self.service+'-init'):
                raise Error('Refusing foreign metrics source containers')
        for identity in identities:
            self.docker.run(['rm', '-f', identity], timeout=20)
        volumes = self.docker.run(['volume', 'ls', '-q', '--filter', 'name=^'+self.config_volume+'$']).split()
        for volume in volumes:
            fact = decode(self.docker.run(['volume', 'inspect', volume]))[0]
            if volume != self.config_volume or (fact.get('Labels') or {}).get(OWNER) != str(ROOT) or (fact.get('Labels') or {}).get(SCOPE) != self.scope:
                raise Error('Refusing foreign metrics source volume')
        for volume in volumes:
            self.docker.run(['volume', 'rm', volume])

    def build(self, files):
        ROOT, OWNER, Error = self.root, self.owner, self.error
        digest = hashlib.sha256()
        with tempfile.TemporaryDirectory(prefix=self.kind+'-source-build-', dir=self.state) as temporary:
            context = Path(temporary)
            for name in files:
                path = ROOT/name
                if path.is_symlink() or not path.is_file():
                    raise Error('Metrics source build input rejected')
                data = path.read_bytes(); digest.update(name.encode()+b'\0'+data)
                target = context/name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
            tag = 'addp-'+self.kind+'-metrics:'+digest.hexdigest()
            self.docker.run(['build', '--label', OWNER+'='+str(ROOT), '-t', tag, '-f', str(context/('scripts/infra/Dockerfile.'+self.kind+'-metrics')), str(context)], timeout=240)
        fact = decode(self.docker.run(['image', 'inspect', tag]))[0]
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', fact['Id']) or (fact.get('Config', {}).get('Labels') or {}).get(OWNER) != str(ROOT):
            raise Error('Metrics source build identity rejected')
        return fact['Id']

    def start(self, database, image, config, ip, port, material, check):
        ROOT, OWNER, SCOPE, Error = self.root, self.owner, self.scope_label, self.error
        files = dict(material, **{'config.json':json.dumps(config).encode()})
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode='w') as tar:
            for name, value in files.items():
                item = tarfile.TarInfo(name); item.size=len(value); item.mode=0o400; item.uid=item.gid=65532
                tar.addfile(item, io.BytesIO(value))
        self.docker.run(['volume','create','--label',OWNER+'='+str(ROOT),'--label',SCOPE+'='+self.scope,self.config_volume])
        volume=decode(self.docker.run(['volume','inspect',self.config_volume]))[0]
        if volume.get('Name')!=self.config_volume or volume.get('Driver')!='local' or (volume.get('Labels') or {}).get(OWNER)!=str(ROOT) or (volume.get('Labels') or {}).get(SCOPE)!=self.scope:
            raise Error('Metrics source private volume ownership changed')
        self.docker.run(['run','--rm','--pull=never','-i','--network','none','--read-only','--cap-drop','ALL','--cap-add','CHOWN','--security-opt','no-new-privileges:true','--memory','64m','--cpus','.25','--pids-limit','32',
            '--label','com.docker.compose.project='+self.project,'--label','com.docker.compose.service='+self.service+'-init','--label',OWNER+'='+str(ROOT),'--label',SCOPE+'='+self.scope,
            '--mount','type=volume,src='+self.config_volume+',dst=/private','--entrypoint','/bin/sh',database.image,'-c','set -eu; base64 -d | tar -xo -C /private; chmod 700 /private; chown -R 65532:65532 /private'],
            input=base64.b64encode(archive.getvalue()).decode())
        labels = {OWNER:str(ROOT), SCOPE:self.scope, self.namespace+'.database-id':database.identity,
                  self.namespace+'.database-started-at':database.facts['started_at']}
        document = {'services':{self.service:{'image':image,'user':'65532:65532','read_only':True,'cap_drop':['ALL'],
            'security_opt':['no-new-privileges:true'],'cpus':.25,'mem_limit':'128m','pids_limit':32,'restart':'no',
            'labels':labels,'networks':['database'],'ports':[{'host_ip':ip,'published':str(port),'target':9443}],
            'volumes':[{'type':'volume','source':'private','target':'/source','read_only':True,'volume':{'nocopy':True}}]}},
            'networks':{'database':{'external':True,'name':database.network}},'volumes':{'private':{'external':True,'name':self.config_volume}}}
        write(self.state/'compose.json',json.dumps(document,sort_keys=True))
        check()
        self.docker.run(['compose','--env-file','/dev/null','-p',self.project,'-f',str(self.state/'compose.json'),'up','-d','--no-deps',self.service],timeout=60)
        check()
        ids=self.docker.run(['ps','-aq','--filter','label=com.docker.compose.project='+self.project,'--filter','label=com.docker.compose.service='+self.service]).split()
        if len(ids)!=1 or not self.docker.fact(ids[0])['running']:
            raise Error('Metrics source did not stay running')
