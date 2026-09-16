#!/usr/bin/env python3
"""Compare real Binder outputs through observation-only Go overlays.

No production source or reference baseline is rewritten. Pointer and Store
adapters share one snapshot schema and one Flow allocation ID space per run.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

HERE = Path(__file__).resolve().parent
TEMPLATE = HERE / 'binder_audit_test.go.tmpl'
FIXTURES = HERE / 'binder_audit_fixtures.json'

POINTER = r'''
type auditNode = *ast.Node
func auditNil(n auditNode) bool { return n == nil }
func auditRoot(sf *ast.SourceFile) auditNode { return sf.AsNode() }
func auditKind(n auditNode) ast.Kind { return n.Kind }
func auditFlags(n auditNode) ast.NodeFlags { return n.Flags }
func auditPos(n auditNode) int { return n.Pos() }
func auditEnd(n auditNode) int { return n.End() }
func auditParent(n auditNode) auditNode { return n.Parent }
func auditFlow(n auditNode) *ast.FlowNode { if d:=n.FlowNodeData();d!=nil{return d.FlowNode};return nil }
func auditEndFlow(n auditNode) *ast.FlowNode { if d:=n.BodyData();d!=nil{return d.EndFlowNode};return nil }
func auditReturnFlow(n auditNode) *ast.FlowNode {
    switch n.Kind {
    case ast.KindConstructor:return n.AsConstructorDeclaration().ReturnFlowNode
    case ast.KindFunctionDeclaration:return n.AsFunctionDeclaration().ReturnFlowNode
    case ast.KindFunctionExpression:return n.AsFunctionExpression().ReturnFlowNode
    case ast.KindClassStaticBlockDeclaration:return n.AsClassStaticBlockDeclaration().ReturnFlowNode
    };return nil
}
func auditFallthroughFlow(n auditNode) *ast.FlowNode {
    if n.Kind==ast.KindCaseClause || n.Kind==ast.KindDefaultClause {return n.AsCaseOrDefaultClause().FallthroughFlowNode};return nil
}
func auditNextContainer(n auditNode) auditNode {if d:=n.LocalsContainerData();d!=nil{return d.NextContainer};return nil}
func auditFlowData(f *ast.FlowNode) *ast.Node {return f.Node}
func auditRelease(sf *ast.SourceFile) {}
func auditLocal(sf *ast.SourceFile) bool {return true}
'''
STORE = r'''
type auditNode = ast.Handle
func auditNil(n auditNode) bool { return n.IsNil() }
func auditRoot(sf *ast.SourceFile) auditNode {s,r:=sf.ParseTreeRef();return s.At(r)}
func auditKind(n auditNode) ast.Kind {return n.Kind}
func auditFlags(n auditNode) ast.NodeFlags {return n.Flags()}
func auditPos(n auditNode) int {return n.Pos()}
func auditEnd(n auditNode) int {return n.End()}
func auditParent(n auditNode) auditNode {return n.Parent()}
func auditFlow(n auditNode) *ast.FlowNode {return n.FlowNode()}
func auditEndFlow(n auditNode) *ast.FlowNode {return n.EndFlowNode()}
func auditReturnFlow(n auditNode) *ast.FlowNode {return n.ReturnFlowNode()}
func auditFallthroughFlow(n auditNode) *ast.FlowNode {return n.FallthroughFlowNode()}
func auditNextContainer(n auditNode) auditNode {return n.NextContainer()}
func auditFlowData(f *ast.FlowNode) *ast.Node {return f.Data}
func auditRelease(sf *ast.SourceFile) {s,_:=sf.ParseTreeRef();ast.UnregisterStore(s)}
func auditLocal(sf *ast.SourceFile) bool {
    s,_:=sf.ParseTreeRef();if s==nil{return false}
    // Reflection only reads map lengths; no private value is extracted or mutated.
    state:=reflect.ValueOf(s).Elem()
    return state.FieldByName("externalChild").Len()==0 && state.FieldByName("externalList").Len()==0
}
'''
MUTATIONS = r'''
func auditMutationChecks(t *testing.T,r *auditRecorder,original auditSnapshot) {
    check:=func(name string,mutate,restore func()) {
        mutate()
        changed:=r.snapshot(original.Fixture)
        restore()
        if reflect.DeepEqual(original,changed) {t.Fatalf("audit missed mutation %s",name)}
        if !reflect.DeepEqual(original.Flows,changed.Flows)||!reflect.DeepEqual(original.Visits,changed.Visits) {t.Fatalf("mutation %s altered graph or visit order",name)}
        if restored:=r.snapshot(original.Fixture);!reflect.DeepEqual(original,restored) {t.Fatalf("mutation %s failed to restore",name)}
        t.Logf("detected attachment mutation: %s",name)
    }
    for _,entry:=range []struct{name string;get func(ast.Handle)*ast.FlowNode;set func(ast.Handle,*ast.FlowNode)}{
        {"Flow",auditFlow,func(n ast.Handle,f *ast.FlowNode){n.SetFlowNode(f)}},
        {"EndFlow",auditEndFlow,func(n ast.Handle,f *ast.FlowNode){n.SetEndFlowNode(f)}},
        {"ReturnFlow",auditReturnFlow,func(n ast.Handle,f *ast.FlowNode){n.SetReturnFlowNode(f)}},
        {"FallthroughFlow",auditFallthroughFlow,func(n ast.Handle,f *ast.FlowNode){n.SetFallthroughFlowNode(f)}},
    } {
        for _,n:=range r.nodes { if previous:=entry.get(n);previous!=nil {
            check(entry.name,func(){entry.set(n,nil)},func(){entry.set(n,previous)});break
        } }
    }
    // A wrong non-nil owner must also be observable with an unchanged Flow graph.
    for _,source:=range r.nodes {if flow:=auditEndFlow(source);flow!=nil {
        target:=auditRoot(r.file);previous:=auditEndFlow(target)
        if source!=target {check("EndFlow wrong owner",func(){target.SetEndFlowNode(flow);source.SetEndFlowNode(nil)},func(){target.SetEndFlowNode(previous);source.SetEndFlowNode(flow)})};break
    }}
    root:=auditRoot(r.file);flags:=root.Flags();next:=root.NextContainer()
    check("Flags",func(){root.Store().SetFlagsAt(root.Ref(),flags^ast.NodeFlagsContainsThis)},func(){root.Store().SetFlagsAt(root.Ref(),flags)})
    if next!=root {check("NextContainer",func(){root.SetNextContainer(root)},func(){root.SetNextContainer(next)})}
    if symbol:=r.file.Symbol;symbol!=nil {check("file.Symbol",func(){r.file.Symbol=nil},func(){r.file.Symbol=symbol})}
}
'''


def sha(data): return hashlib.sha256(data).hexdigest()
def dump(path,value): Path(path).write_text(json.dumps(value,ensure_ascii=False,indent=2,sort_keys=True)+'\n')
def source_manifest(repo):
    paths=list((repo/'tsc/internal').rglob('*.go'))
    paths.extend(repo/p for p in ['go.work','go.work.sum','tsc/go.mod','tsc/go.sum'])
    return {str(p.relative_to(repo)):sha(p.read_bytes()) for p in sorted(paths) if p.is_file()}

def instrument(source,representation):
    if representation=='pointer':
        anchor='func (b *Binder) bind(node *ast.Node) bool {';value='node'
    else:
        candidates=[('func (b *Binder) bind(node ast.NodeRef) bool {','b.store.At(node)'),('func (b *Binder) bind(ref ast.NodeRef) bool {','b.store.At(ref)'),('func (b *Binder) bindKind(id ast.NodeRef, kind ast.Kind, parentKind ast.Kind) bool {','b.store.At(id)')]
        found=[pair for pair in candidates if pair[0] in source]
        if len(found)!=1:raise ValueError('Expected exactly one Store bind observation anchor')
        anchor,value=found[0]
    if source.count(anchor)!=1:raise ValueError('Ambiguous bind observation anchor')
    source=source.replace(anchor,anchor+'\n if foundationAudit != nil { foundationAudit.visit('+value+') }',1)
    anchor='func (b *Binder) newFlowNode(flags ast.FlowFlags) *ast.FlowNode {'
    if source.count(anchor)!=1:raise ValueError('Missing Flow allocation observation anchor')
    source=source.replace(anchor,'func (b *Binder) foundationAuditNewFlowNode(flags ast.FlowFlags) *ast.FlowNode {',1)
    source+='''\nfunc (b *Binder) newFlowNode(flags ast.FlowFlags) *ast.FlowNode {
 f := b.foundationAuditNewFlowNode(flags)
 if foundationAudit != nil { foundationAudit.flows = append(foundationAudit.flows, f) }
 return f
}\n'''
    return source


def run(args):
    repo=Path(args.repo).resolve();out=Path(args.out).resolve();out.mkdir(parents=True,exist_ok=False)
    representation=args.representation
    fixture=Path(args.fixtures).resolve();fixture_copy=out/'fixtures.json';fixture_copy.write_bytes(fixture.read_bytes())
    sources=source_manifest(repo)
    info={'schema':2,'repo_root':str(repo),'representation':representation,'sources':sources,'fixtures_sha256':sha(fixture_copy.read_bytes()),'template_sha256':sha(TEMPLATE.read_bytes()),'driver_sha256':sha(Path(__file__).read_bytes()),'ready':False}
    dump(out/'identity.json',info)
    overlays=out/'overlay-sources';overlays.mkdir()
    binder=overlays/'binder.go';binder.write_text(instrument((repo/'tsc/internal/binder/binder.go').read_text(),representation))
    rendered=TEMPLATE.read_text().replace('@ADAPTER@',POINTER if representation=='pointer' else STORE).replace('@MUTATION_CHECKS@','func auditMutationChecks(t *testing.T,r *auditRecorder,s auditSnapshot) {}' if representation=='pointer' else MUTATIONS)
    test=overlays/'foundation_semantic_audit_test.go';test.write_text(rendered)
    replacements={str(repo/'tsc/internal/binder/binder.go'):str(binder),str(repo/'tsc/internal/binder/foundation_semantic_audit_overlay_test.go'):str(test)}
    subprocess.run(['gofmt','-w',*[str(p) for p in overlays.glob('*.go')]],check=True)
    info['overlay_sha256']={p.name:sha(p.read_bytes()) for p in overlays.iterdir()};dump(out/'overlay.json',{'Replace':replacements})
    env=os.environ.copy();env.update(GOFLAGS='',GOWORK=str(repo/'go.work'),BINDER_AUDIT_FIXTURES=str(fixture_copy),BINDER_AUDIT_OUT=str(out/'snapshots'))
    info['go_version']=subprocess.check_output(['go','version'],text=True).strip()
    binary=out/'audit.test';cmd=['go','test','-c','-overlay',str(out/'overlay.json'),'-o',str(binary),'./internal/binder'];info['build_command']=cmd
    with (out/'build.log').open('wb') as log:code=subprocess.run(cmd,cwd=repo/'tsc',env=env,stdout=log,stderr=subprocess.STDOUT).returncode
    info['build_exit_code']=code
    if code==0:
        info['binary_sha256']=sha(binary.read_bytes())
        with (out/'test.log').open('wb') as log:code=subprocess.run([str(binary),'-test.run=^TestFoundationSemanticAudit$','-test.v','-test.timeout=5m'],cwd=repo/'tsc',env=env,stdout=log,stderr=subprocess.STDOUT).returncode
        info['test_exit_code']=code
    info['sources_stable']=sources==source_manifest(repo)
    if not info['sources_stable']:code=2
    expected={f['Name']+'.json' for f in json.loads(fixture_copy.read_text())}
    actual={p.name for p in (out/'snapshots').glob('*.json')}
    info['snapshot_sha256']={p.name:sha(p.read_bytes()) for p in sorted((out/'snapshots').glob('*.json'))}
    info['complete']=code==0 and actual==expected
    info['ready']=info['complete'];dump(out/'identity.json',info)
    print(json.dumps({'out':str(out),'complete':info['complete'],'exit_code':code,'snapshots':len(actual)}))
    return 0 if info['complete'] else (code or 2)


def differences(a,b,path='$'):
    if type(a)!=type(b):return [{'path':path,'old':a,'new':b}]
    if isinstance(a,dict):
        rows=[]
        for key in sorted(a.keys()|b.keys()):
            if key not in a or key not in b:rows.append({'path':path+'.'+key,'old':a.get(key),'new':b.get(key)})
            else:rows.extend(differences(a[key],b[key],path+'.'+key))
        return rows
    if isinstance(a,list):
        if len(a)!=len(b):return [{'path':path+'.length','old':len(a),'new':len(b)}]
        return [row for i,(av,bv) in enumerate(zip(a,b)) for row in differences(av,bv,f'{path}[{i}]')]
    return [] if a==b else [{'path':path,'old':a,'new':b}]


def compare(args):
    old,new=Path(args.old),Path(args.new);out=Path(args.out);out.mkdir(parents=True,exist_ok=False)
    oi,ni=(json.loads((p/'identity.json').read_text()) for p in (old,new))
    if not oi.get('complete') or not ni.get('complete'):raise ValueError('Incomplete audit; cannot compare')
    for directory,identity in [(old,oi),(new,ni)]:
        actual={p.name:sha(p.read_bytes()) for p in sorted((directory/'snapshots').glob('*.json'))}
        if actual!=identity.get('snapshot_sha256'):raise ValueError('Snapshot content changed or digest manifest is missing: '+str(directory))
    if oi['fixtures_sha256']!=ni['fixtures_sha256']:raise ValueError('Input fixtures differ')
    if oi['template_sha256']!=ni['template_sha256'] or oi['driver_sha256']!=ni['driver_sha256']:raise ValueError('Audit observation code differs')
    summary={};total=0
    for oldfile in sorted((old/'snapshots').glob('*.json')):
        a=json.loads(oldfile.read_text());b=json.loads((new/'snapshots'/oldfile.name).read_text())
        # Syntax mismatches are recorded separately; they are not silently normalized away.
        shape=lambda s:[{k:row[k] for k in ['Key','Kind','Pos','End','BeforeFlags','Children','Parent']} for row in s['Nodes']]
        shape_diff=differences(shape(a),shape(b))
        diff=differences(a,b);total+=len(diff)
        summary[oldfile.stem]={'differences':len(diff),'syntax_differences':len(shape_diff)}
        dump(out/oldfile.name,{'syntax_differences':shape_diff,'differences':diff})
    dump(out/'summary.json',{'fixtures':summary,'differences':total,'old':str(old),'new':str(new)})
    print(json.dumps({'fixtures':len(summary),'differences':total,'out':str(out)}))
    return 1 if total else 0


def selftest():
    sample={'Nodes':[{'Key':'root/c0','Flow':1,'EndFlow':2,'ReturnFlow':3,'FallthroughFlow':4,'Flags':8,'NextContainer':'root/c1'}],'Flows':[{'ID':1,'Antecedent':0}],'Visits':['root/c0'],'FileSymbol':1}
    for field in ['Flow','EndFlow','ReturnFlow','FallthroughFlow','Flags','NextContainer']:
        changed=copy.deepcopy(sample);changed['Nodes'][0][field]=0 if field!='NextContainer' else ''
        diff=differences(sample,changed);assert len(diff)==1 and diff[0]['path'].endswith('.'+field)
    changed=copy.deepcopy(sample);changed['FileSymbol']=0;assert differences(sample,changed)[0]['path']=='$.FileSymbol'
    assert differences(sample,copy.deepcopy(sample))==[]
    print('Comparator selftest: attachments, Flags, NextContainer, and file.Symbol detected')
    return 0


def main():
    parser=argparse.ArgumentParser(description=__doc__);subs=parser.add_subparsers(dest='command',required=True)
    p=subs.add_parser('run');p.add_argument('--repo',required=True);p.add_argument('--representation',choices=['pointer','store'],required=True);p.add_argument('--out',required=True);p.add_argument('--fixtures',default=str(FIXTURES))
    p=subs.add_parser('compare');p.add_argument('--old',required=True);p.add_argument('--new',required=True);p.add_argument('--out',required=True)
    subs.add_parser('selftest');args=parser.parse_args()
    return run(args) if args.command=='run' else compare(args) if args.command=='compare' else selftest()

if __name__=='__main__':
    try:sys.exit(main())
    except (OSError,ValueError,subprocess.CalledProcessError) as error:print(str(error),file=sys.stderr);sys.exit(2)
