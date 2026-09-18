#!/usr/bin/env python3
"""Check only this design increment; never invoke product services or experiments."""
from pathlib import Path
import datetime, difflib, hashlib, json, os, re, subprocess, tempfile, unicodedata, zipfile
ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
meta = json.loads((OUT/'source-evidence.json').read_text())
base = json.loads((OUT/'baseline.json').read_text())
archive = zipfile.ZipFile(OUT/meta['originalDocumentsArchive'])
def original_text(name):
    return archive.read(name).decode('utf-8').replace('\r\n','\n')
allowed = {f'docs/current/{name}' for name in meta['plannedCurrentDocs']}
checks = {'checkedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'scope': 'design artifacts only', 'runtimeCases': 'NOT_RUN'}
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
changed, violated = [], []
for row in base['files']:
    path = ROOT/row['path']
    if not path.exists() or sha(path) != row['sha256']:
        changed.append(row['path'])
        if row['path'] not in allowed: violated.append(row['path'])
checks['originalArchive']={'documents':len(meta['plannedCurrentDocs']),'hashesMatch':all(hashlib.sha256(archive.read(name)).hexdigest()==next(row['sha256'] for row in base['files'] if row['path']=='docs/current/'+name) for name in meta['plannedCurrentDocs'])}
checks['protectedBaseline'] = {'checked': len(base['files']), 'allowedChanged': changed, 'violations': violated}
# Compare task-specific edits against captured workspace, not HEAD's older B02/B03 tree.
diff = []
added_lines = {}
for name in meta['plannedCurrentDocs']:
    p = ROOT/'docs/current'/name; rel = str(p.relative_to(ROOT))
    old = original_text(name).splitlines(keepends=True); new = p.read_text().splitlines(keepends=True)
    diff.extend(difflib.unified_diff(old,new,fromfile='a/'+rel,tofile='b/'+rel))
    added_lines[p] = []
    for tag,a,b,c,d in difflib.SequenceMatcher(a=old,b=new,autojunk=False).get_opcodes():
        if tag != 'equal': added_lines[p].extend((i+1,new[i]) for i in range(c,d))
for p in sorted(OUT.glob('*.md')):
    rel = str(p.relative_to(ROOT)); new = p.read_text().splitlines(keepends=True)
    diff.extend(difflib.unified_diff([],new,fromfile='/dev/null',tofile='b/'+rel))
    added_lines[p] = list(enumerate(new,1))
for ext in ('*.go.txt','*.ts.txt','verify-design.py'):
    for p in sorted(OUT.glob(ext)):
        rel = str(p.relative_to(ROOT))
        diff.extend(difflib.unified_diff([],p.read_text().splitlines(keepends=True),fromfile='/dev/null',tofile='b/'+rel))
(OUT/'changes.diff').write_text(''.join(diff))
# Local link/anchor checks on new text only; previous unrelated broken links are not rewritten.
def slug(s):
    s=re.sub(r'`([^`]+)`',r'\1',s.strip()).lower()
    s=re.sub(r'\[([^]]+)\]\([^)]*\)',r'\1',s)
    s=''.join(c for c in s if c in '-_ ' or not unicodedata.category(c).startswith(('P','S')))
    return s.replace(' ','-')
def anchors(p):
    result=set(); counts={}
    for line in p.read_text().splitlines():
        if re.match(r'^#{1,6} ',line):
            value=slug(re.sub(r'^#{1,6} ','',line)); n=counts.get(value,0); counts[value]=n+1
            result.add(value+(f'-{n}' if n else ''))
    return result
errors=[]; links=0
for p,lines in added_lines.items():
    for lineno,line in lines:
        for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)',line):
            if re.match(r'^[a-zA-Z]+://',target): continue
            target=target.strip('<>'); name,sep,anchor=target.partition('#')
            dest=(p.parent/name).resolve() if name else p
            links+=1
            if not dest.exists(): errors.append({'file':str(p.relative_to(ROOT)),'line':lineno,'target':target,'error':'missing_path'})
            elif sep and dest.suffix=='.md' and anchor not in anchors(dest): errors.append({'file':str(p.relative_to(ROOT)),'line':lineno,'target':target,'error':'missing_anchor'})
checks['newLocalLinks']={'count':links,'errors':errors,'anchorMethod':'heading Unicode punctuation stripping; no browser renderer run'}
# Actual Go parser accepts declaration-only functions. Type checking product packages is deferred.
parser_source=r'''package main
import ("encoding/json"; "go/parser"; "go/token"; "os"; "regexp"; "fmt")
func main(){ n:=0; packages:=[]string{}; for _,p:=range os.Args[1:] {b,e:=os.ReadFile(p);if e!=nil{panic(e)};r:=regexp.MustCompile(`(?m)^package [a-zA-Z_][a-zA-Z_0-9]*`);m:=r.FindAllIndex(b,-1);for i,v:=range m{end:=len(b);if i+1<len(m){end=m[i+1][0]};f,e:=parser.ParseFile(token.NewFileSet(),p,b[v[0]:end],parser.AllErrors);if e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)};n+=len(f.Decls);packages=append(packages,f.Name.Name)}};json.NewEncoder(os.Stdout).Encode(map[string]any{"packageSections":len(packages),"declarations":n,"packages":packages})}
'''
with tempfile.TemporaryDirectory(prefix='repomesh-design-check-') as td:
    td=Path(td); parser=td/'parse.go';parser.write_text(parser_source)
    r=subprocess.run(['go','run',str(parser),*[str(x) for x in sorted(OUT.glob('*.go.txt'))]],cwd=td,capture_output=True,text=True,env={**os.environ,'GOPROXY':'off','GOTOOLCHAIN':'local','GO111MODULE':'off'})
    checks['goDeclarationSyntax']={'exitCode':r.returncode,'output':r.stdout.strip(),'errors':r.stderr.strip(),'typeCheck':'NOT_RUN: multi-package declarations have no product bodies'}
    ts=td/'design.ts';ts.write_text((OUT/'frontend.ts.txt').read_text().replace('../../../web/src/api',str(ROOT/'web/src/api')))
    args=['node',str(ROOT/'web/node_modules/typescript/bin/tsc'),'--strict','--noEmit','--skipLibCheck','--target','ES2022','--module','ESNext','--moduleResolution','bundler','--lib','ES2022,DOM',str(ts)]
    r=subprocess.run(args,cwd=td,capture_output=True,text=True)
    checks['typescriptDeclarations']={'exitCode':r.returncode,'output':r.stdout.strip(),'errors':r.stderr.strip(),'command':'existing local tsc --strict --noEmit --skipLibCheck --target ES2022 --module ESNext --moduleResolution bundler --lib ES2022,DOM temporary-design.ts'}
with tempfile.TemporaryDirectory(prefix='repomesh-design-whitespace-') as td:
    td=Path(td); old=td/'old';new=td/'new';old.mkdir();new.mkdir(); (old/'current').mkdir(); (new/'current').mkdir(); (new/'attachments').mkdir()
    for name in meta['plannedCurrentDocs']:
        (old/'current'/name).write_text(original_text(name)); (new/'current'/name).write_text((ROOT/'docs/current'/name).read_text())
    for p in OUT.iterdir():
        if p.suffix=='.md' or p.name.endswith(('.go.txt','.ts.txt')) or p.name=='verify-design.py': (new/'attachments'/p.name).write_bytes(p.read_bytes())
    r=subprocess.run(['git','-c','core.autocrlf=false','-c','core.safecrlf=false','diff','--no-index','--check',str(old),str(new)],capture_output=True,text=True)
    checks['taskWhitespace']={'exitCode':r.returncode,'output':r.stdout.strip(),'errors':r.stderr.strip(),'passed':r.returncode in (0,1) and not r.stdout.strip() and not r.stderr.strip(),'note':'no-index can return 1 for changed paths; whitespace violations produce diagnostics'}
status=subprocess.run(['git','status','--porcelain=v1','--untracked-files=all'],cwd=ROOT,capture_output=True,text=True,check=True).stdout
checks['gitState']={'head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),'branch':subprocess.check_output(['git','branch','--show-current'],cwd=ROOT,text=True).strip(),'initialStatusPreserved':None}
initial=(OUT/'initial-status.txt').read_text().splitlines(); now=status.splitlines()
newpaths=[]
for row in now:
    if row not in initial and not row[3:].startswith(str(OUT.relative_to(ROOT))+'/') and row[3:] not in allowed: newpaths.append(row)
checks['gitState']['unexpectedNewStatus']=newpaths
missing_status=[row for row in initial if row not in now and row[3:] not in allowed and not row[3:].startswith(str(OUT.relative_to(ROOT))+'/')]
checks['gitState']['preexistingStatusChanges']=missing_status
checks['gitState']['initialStatusPreserved']=not missing_status
checks['gitState']['excludedConfigBytes']='NOT_READ: protected by no-write scope and status comparison, not content hashes'
checks['passed']=checks['originalArchive']['hashesMatch'] and not violated and not errors and checks['goDeclarationSyntax']['exitCode']==0 and checks['typescriptDeclarations']['exitCode']==0 and checks['taskWhitespace']['passed'] and not newpaths and not missing_status
(OUT/'checks.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n')
manifest=[]
for p in sorted(OUT.iterdir()):
    if p.is_file() and p.name!='output-manifest.json': manifest.append({'path':str(p.relative_to(ROOT)),'sha256':sha(p)})
for rel in sorted(allowed): manifest.append({'path':rel,'sha256':sha(ROOT/rel)})
(OUT/'output-manifest.json').write_text(json.dumps({'files':manifest,'note':'Manifest excludes itself; source baseline remains separate.'},ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:v for k,v in checks.items() if k!='protectedBaseline'},ensure_ascii=False,indent=2))
print('protected source violations:',violated)
raise SystemExit(0 if checks['passed'] else 1)
