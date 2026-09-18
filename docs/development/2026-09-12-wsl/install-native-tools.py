import hashlib, json, pathlib, subprocess, urllib.request
base = pathlib.Path('/opt/repomesh-dev-tools')
base.mkdir(exist_ok=True)
def fetch(url):
    with urllib.request.urlopen(url, timeout=120) as response:
        return response.read()
def install(name, version, archive, url, digest, directory, links):
    target = base / directory
    if target.exists():
        raise RuntimeError('Refusing to overwrite ' + str(target))
    data = fetch(url)
    actual = hashlib.sha256(data).hexdigest()
    if actual != digest:
        raise RuntimeError('Checksum mismatch for ' + archive)
    download = base / archive
    download.write_bytes(data)
    subprocess.run(['tar', '-xf', str(download), '-C', str(base)], check=True)
    for command, relative in links.items():
        link = pathlib.Path('/usr/local/bin') / command
        if link.exists() or link.is_symlink():
            raise RuntimeError('Refusing to overwrite ' + str(link))
        link.symlink_to(target / relative)
    return {'name':name, 'version':version, 'url':url, 'sha256':actual, 'path':str(target)}
go_all = json.loads(fetch('https://go.dev/dl/?mode=json&include=all'))
release = next(r for r in go_all if r['version']=='go1.26.4')
go = next(f for f in release['files'] if f['filename']=='go1.26.4.linux-amd64.tar.gz')
records = [install('go', '1.26.4', go['filename'], 'https://go.dev/dl/'+go['filename'], go['sha256'], 'go', {'go':'bin/go','gofmt':'bin/gofmt'})]
node_archive='node-v22.22.1-linux-x64.tar.xz'
node_url='https://nodejs.org/dist/v22.22.1/'
node_sha=next(line.split()[0] for line in fetch(node_url+'SHASUMS256.txt').decode().splitlines() if line.split()[-1]==node_archive)
records.append(install('node','22.22.1',node_archive,node_url+node_archive,node_sha,'node-v22.22.1-linux-x64',{'node':'bin/node','npm':'bin/npm','npx':'bin/npx','corepack':'bin/corepack'}))
pathlib.Path('/mnt/d/Project4work/Repomesh_Go_ver/docs/development/2026-09-12-wsl/native-tool-sources.json').write_text(json.dumps(records,indent=2)+'\n')
print(json.dumps(records,indent=2))
