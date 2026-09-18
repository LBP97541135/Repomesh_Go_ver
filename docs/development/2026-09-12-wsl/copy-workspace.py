import hashlib, json, os, pathlib, shutil, subprocess, sys
os.environ["GIT_OPTIONAL_LOCKS"]="0"
source=pathlib.Path('/mnt/d/Project4work/Repomesh_Go_ver')
target=pathlib.Path('/home/xubohan/projects/Repomesh_Go_ver')
archive=target.parent/'repomesh-windows-worktree-metadata-20260912'
evidence=source/'docs/development/2026-09-12-wsl'
if (target.exists() or archive.exists()) and '--resume-owned-copy' not in sys.argv: raise RuntimeError('Target/archive already exists')
def git(path,*args):
    return subprocess.check_output(['git','-c','safe.directory='+str(path),'-c','core.autocrlf=true','-C',str(path),*args],stderr=subprocess.PIPE).decode()
def digest(path):
    h=hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda:f.read(1024*1024),b''): h.update(block)
    return h.hexdigest()
def snapshot(path):
    return dict(head=git(path,'rev-parse','HEAD').strip(),branch=git(path,'branch','--show-current').strip(),indexEntriesSha256=hashlib.sha256(git(path,'ls-files','--stage').encode()).hexdigest(),staged=git(path,'diff','--cached','--name-status'),status=git(path,'status','--porcelain=v1','-uall'))
before=snapshot(source)
upstream=snapshot(source/'third_party/AgentTeams')
excluded=[]; records=[]
target.mkdir(parents=True,exist_ok=True)
skip_roots={'bin','dist','web/dist','.playwright-cli','.playwright-mcp','output/playwright','.git/worktrees'}
skip_names={'node_modules','__pycache__','.venv'}
for folder,dirs,files in os.walk(source,followlinks=False):
    rel=pathlib.Path(folder).relative_to(source)
    for name in list(dirs):
        item=(rel/name).as_posix()
        source_dir=pathlib.Path(folder)/name
        if source_dir.is_symlink():
            link=os.readlink(source_dir); destination_link=target/rel/name
            destination_link.parent.mkdir(parents=True,exist_ok=True)
            if destination_link.is_symlink():
                if os.readlink(destination_link)!=link: raise RuntimeError('Different directory symlink '+item)
            elif destination_link.exists(): raise RuntimeError('Expected directory symlink '+item)
            else: destination_link.symlink_to(link)
            records.append(dict(path=item,symlink=link)); dirs.remove(name); continue
        if item in skip_roots or name in skip_names:
            excluded.append(item); dirs.remove(name)
    destination=target/rel
    destination.mkdir(exist_ok=True)
    for name in files:
        src=pathlib.Path(folder)/name; relative=(rel/name).as_posix(); dst=destination/name
        if src.is_symlink():
            link=os.readlink(src)
            if dst.is_symlink():
                if os.readlink(dst)!=link: raise RuntimeError('Different existing link '+relative)
            elif dst.exists(): raise RuntimeError('Expected symlink '+relative)
            else: dst.symlink_to(link)
            records.append(dict(path=relative,symlink=link)); continue
        stat_before=src.stat()
        contents=src.read_bytes()
        a=hashlib.sha256(contents).hexdigest()
        if src.stat().st_mtime_ns!=stat_before.st_mtime_ns: raise RuntimeError('Source changed '+relative)
        if not dst.exists() or digest(dst)!=a: dst.write_bytes(contents)
        if digest(dst)!=a: raise RuntimeError('Copy changed: '+relative)
        dst.chmod(0o600 if relative.startswith('.git/') or name.startswith('.env') else 0o644)
        records.append(dict(path=relative,bytes=src.stat().st_size,sha256=a))
if (source/'.git/worktrees').exists(): shutil.copytree(source/'.git/worktrees',archive,dirs_exist_ok=True)
after=snapshot(source)
if before!=after: raise RuntimeError('Source Git state changed during copy')
copied=snapshot(target)
if before!=copied: raise RuntimeError('Copied Git state differs')
if upstream!=snapshot(target/'third_party/AgentTeams'): raise RuntimeError('Upstream Git differs')
# Preserve the source checkout's newline interpretation while enabling Linux case-sensitive paths.
for path in [target,target/'third_party/AgentTeams']:
    subprocess.run(['git','-c','safe.directory='+str(path),'-C',str(path),'config','core.autocrlf','true'],check=True)
    subprocess.run(['git','-c','safe.directory='+str(path),'-C',str(path),'config','core.ignorecase','false'],check=True)
    subprocess.run(['git','-c','safe.directory='+str(path),'-C',str(path),'config','core.symlinks','true'],check=True)
for path in [target,target/'third_party/AgentTeams']:
    for entry in git(path,'ls-files','--stage','-z').split('\0'):
        if not entry: continue
        meta, name=entry.split('\t',1)
        item=path/name
        if item.is_file() and not item.is_symlink():
            item.chmod(0o755 if meta.startswith('100755 ') else 0o644)
    subprocess.run(['git','-c','safe.directory='+str(path),'-C',str(path),'config','core.filemode','true'],check=True)
configuration=target/'.codex/config.toml'
configuration.write_text(configuration.read_text().replace('D:/Project4work/Repomesh_Go_ver',str(target)))
report=dict(result='VERIFIED',source=str(source),target=str(target),baseline=before,upstream=upstream,files=len(records),bytes=sum(r.get('bytes',0) for r in records),excluded=excluded,worktreeMetadataArchive=str(archive),targetAdjustments=['.git/config: core.autocrlf=true, core.ignorecase=false, core.symlinks=true, core.filemode=true; indexed executable modes restored','third_party/AgentTeams/.git/config: same platform settings','.codex/config.toml: Linux marketplace path'],records=records)
for folder, dirs, files in os.walk(target):
    os.chown(folder,1000,1000)
    for name in files: os.chown(pathlib.Path(folder)/name,1000,1000,follow_symlinks=False)
final_root=snapshot(target)
final_upstream=snapshot(target/'third_party/AgentTeams')
for key in ['head','branch','indexEntriesSha256','staged']:
    if final_root[key]!=before[key]: raise RuntimeError('Root Git state differs after adaptation: '+key)
def without_platform_status(value):
    return '\n'.join(line for line in value.splitlines() if line[3:] not in ['.codex/config.toml','docs/development/2026-09-12-wsl/copy-manifest.json'])
if without_platform_status(final_root['status'])!=without_platform_status(before['status']):
    raise RuntimeError('Unexpected root worktree drift after adaptation')
if final_upstream!=upstream: raise RuntimeError('Upstream drift after adaptation')
for path in [target,target/'third_party/AgentTeams']:
    for entry in git(path,'ls-files','--stage','-z').split('\0'):
        if not entry: continue
        meta,name=entry.split('\t',1); item=path/name
        if item.is_file() and not item.is_symlink():
            expected=0o755 if meta.startswith('100755 ') else 0o644
            if item.stat().st_mode & 0o777 != expected: raise RuntimeError('Incorrect file mode '+str(item))
report['postAdaptationStateVerified']=True
(evidence/'copy-manifest.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
shutil.copyfile(evidence/'copy-manifest.json',target/'docs/development/2026-09-12-wsl/copy-manifest.json')
print(json.dumps({k:v for k,v in report.items() if k not in ['records','baseline','upstream']},indent=2))
