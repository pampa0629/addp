"""TLS material and HTTPS requests for disposable metrics-source tests only."""
import os
import ssl
import subprocess
import urllib.error
import urllib.request

def command(args,**kwargs):
    return subprocess.run(args,check=True,capture_output=True,timeout=30,**kwargs)


def certificates(root):
    config=root/'request.cnf';config.write_text('[req]\nprompt=no\ndistinguished_name=dn\n[dn]\nCN=metrics-source-t2\n')
    environment=dict(os.environ,OPENSSL_CONF=str(config))
    def openssl(*args):command(['openssl',*args],env=environment)
    openssl('req','-x509','-newkey','rsa:2048','-nodes','-days','1','-config',str(config),'-subj','/CN=metrics-source-t2-ca','-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign','-addext','subjectKeyIdentifier=hash','-keyout',str(root/'ca.key'),'-out',str(root/'ca.crt'))
    for name,purpose in [('server','serverAuth'),('client','clientAuth')]:
        openssl('req','-newkey','rsa:2048','-nodes','-config',str(config),'-subj','/CN=metrics-source-t2-'+name,'-keyout',str(root/(name+'.key')),'-out',str(root/(name+'.csr')))
        extension=root/(name+'.ext');extension.write_text('basicConstraints=critical,CA:FALSE\nauthorityKeyIdentifier=keyid:always\nsubjectKeyIdentifier=hash\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage='+purpose+'\nsubjectAltName=IP:127.0.0.1\n')
        openssl('x509','-req','-in',str(root/(name+'.csr')),'-CA',str(root/'ca.crt'),'-CAkey',str(root/'ca.key'),'-CAcreateserial','-days','1','-extfile',str(extension),'-out',str(root/(name+'.crt')))
    for name in ('server.key','client.key','ca.key'): (root/name).chmod(0o600)
    tls=root/'source-tls';tls.mkdir(mode=0o700)
    for name in ('ca.crt','server.crt','server.key'):
        (tls/name).write_bytes((root/name).read_bytes());(tls/name).chmod(0o600)
    # Child verification uses the same isolated minimal configuration.
    os.environ['OPENSSL_CONF']=str(config)
    return tls


def context(root,client=True,wrong=False):
    ctx=ssl.create_default_context(cafile=str(root/'ca.crt'))
    if client:
        name='server' if wrong else 'client'
        ctx.load_cert_chain(str(root/(name+'.crt')),str(root/(name+'.key')))
    return ctx


def request(port,ctx,path='/metrics',method='GET'):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=ctx))
    try:
        with opener.open(urllib.request.Request(f'https://127.0.0.1:{port}'+path,method=method),timeout=5) as response:
            return response.status,response.read(32769).decode()
    except urllib.error.HTTPError as error:return error.code,error.read(32769).decode()
